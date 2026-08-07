package runtime

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/serviceruntime"
	"github.com/codex-k8s/matter-codex/services/external/egress-gateway/internal/dnsresolver"
	"github.com/codex-k8s/matter-codex/services/external/egress-gateway/internal/gateway"
	"github.com/codex-k8s/matter-codex/services/external/egress-gateway/internal/observability"
	"github.com/codex-k8s/matter-codex/services/external/egress-gateway/internal/policy"
)

// Run материализует startup barrier, readiness refresh и ordered drain.
func Run(lifecycle, shutdownBase context.Context, config Config, activePolicy *policy.Active) error {
	runContext, cancelRun := context.WithCancel(lifecycle)
	defer cancelRun()
	metrics := observability.NewMetrics()
	servers, err := dnsresolver.LoadSystemServers(config.ResolverConfig)
	if err != nil {
		return runTechnicalOnly(lifecycle, shutdownBase, config.TechnicalAddress, newDegradedState(activePolicy, metrics), metrics)
	}
	var runtimeState *state
	resolver, err := dnsresolver.New(activePolicy.DNS(), servers, nil, func(outcome string, reason dnsresolver.Reason) {
		metrics.DNSObserver(outcome, string(reason))
		if outcome == "rejected" && runtimeState != nil {
			runtimeState.setResolverReady(false)
			runtimeState.setProcess(processNotReady)
		}
	})
	if err != nil {
		return runTechnicalOnly(lifecycle, shutdownBase, config.TechnicalAddress, newDegradedState(activePolicy, metrics), metrics)
	}
	runtimeState = newState(activePolicy, metrics)
	connectServer, err := gateway.New(runContext, config.ConnectAddress, activePolicy, resolver, &gateway.NetDialer{}, metrics)
	if err != nil {
		return err
	}
	technical := newTechnicalServer(config.TechnicalAddress, runtimeState, metrics)
	if err := technical.listen(); err != nil {
		return err
	}
	if err := connectServer.Listen(); err != nil {
		_ = technical.listener.Close()
		return err
	}

	errorsChannel := make(chan error, 3)
	var workers sync.WaitGroup
	workers.Add(1)
	go func() { defer workers.Done(); errorsChannel <- technical.serve() }()

	startupInterval := time.Duration(activePolicy.DNS().MinimumTTLSeconds) * time.Second
	for {
		if err := preflight(runContext, resolver, activePolicy); err == nil {
			runtimeState.setResolverReady(true)
			break
		}
		runtimeState.setProcess(processNotReady)
		timer := time.NewTimer(startupInterval)
		select {
		case <-lifecycle.Done():
			timer.Stop()
			cancelRun()
			return shutdownActive(shutdownBase, activePolicy, connectServer, technical, runtimeState, &workers, nil)
		case technicalErr := <-errorsChannel:
			timer.Stop()
			cancelRun()
			return shutdownActive(shutdownBase, activePolicy, connectServer, technical, runtimeState, &workers, technicalErr)
		case <-timer.C:
		}
	}

	runtimeState.setProcess(processReady)
	workers.Add(2)
	go func() { defer workers.Done(); errorsChannel <- connectServer.Serve() }()
	go func() {
		defer workers.Done()
		errorsChannel <- refresh(runContext, resolver, activePolicy, runtimeState)
	}()

	var runErr error
	select {
	case <-lifecycle.Done():
	case runErr = <-errorsChannel:
	}
	cancelRun()
	return shutdownActive(shutdownBase, activePolicy, connectServer, technical, runtimeState, &workers, runErr)
}

// RunInvalidPolicy сохраняет безопасный readback, но никогда не открывает CONNECT listener.
func RunInvalidPolicy(lifecycle, shutdownBase context.Context, config Config) error {
	metrics := observability.NewMetrics()
	return runTechnicalOnly(lifecycle, shutdownBase, config.TechnicalAddress, newInvalidPolicyState(metrics), metrics)
}

func runTechnicalOnly(lifecycle, shutdownBase context.Context, address string, state *state, metrics *observability.Metrics) error {
	technical := newTechnicalServer(address, state, metrics)
	if err := technical.listen(); err != nil {
		return err
	}
	serveResult := make(chan error, 1)
	go func() { serveResult <- technical.serve() }()
	var runErr error
	serveFinished := false
	select {
	case <-lifecycle.Done():
	case runErr = <-serveResult:
		serveFinished = true
	}
	state.setProcess(processDraining)
	operations := []serviceruntime.ShutdownOperation{
		serviceruntime.ShutdownOperation{Name: "technical server shutdown", Timeout: 5 * time.Second, Run: technical.shutdown},
	}
	if !serveFinished {
		operations = append(operations, serviceruntime.ShutdownOperation{Name: "technical server join", Timeout: 5 * time.Second, Run: func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				return errors.New("technical server join deadline exceeded")
			case serveErr := <-serveResult:
				return serveErr
			}
		}})
	}
	shutdownErr := serviceruntime.RunShutdown(shutdownBase, operations...)
	state.setProcess(processStopped)
	return errors.Join(runErr, shutdownErr)
}

func shutdownActive(
	shutdownBase context.Context,
	activePolicy *policy.Active,
	connectServer *gateway.Server,
	technical *technicalServer,
	state *state,
	workers *sync.WaitGroup,
	runErr error,
) error {
	state.setProcess(processDraining)
	shutdownTimeout := time.Duration(activePolicy.Limits().ShutdownTimeoutMilliseconds) * time.Millisecond
	shutdownErr := serviceruntime.RunShutdown(shutdownBase,
		serviceruntime.ShutdownOperation{Name: "CONNECT server drain", Timeout: shutdownTimeout, Run: connectServer.Shutdown},
		serviceruntime.ShutdownOperation{Name: "technical server shutdown", Timeout: 5 * time.Second, Run: technical.shutdown},
		serviceruntime.ShutdownOperation{Name: "runtime worker join", Timeout: 5 * time.Second, Run: func(ctx context.Context) error {
			done := make(chan struct{})
			go func() { workers.Wait(); close(done) }()
			select {
			case <-ctx.Done():
				return errors.New("runtime worker join deadline exceeded")
			case <-done:
				return nil
			}
		}},
	)
	state.setProcess(processStopped)
	return errors.Join(runErr, shutdownErr)
}

func preflight(ctx context.Context, resolver *dnsresolver.Resolver, activePolicy *policy.Active) error {
	for _, destination := range activePolicy.Destinations() {
		if _, err := resolver.Resolve(ctx, destination.Hostname); err != nil {
			return errors.New("DNS readiness preflight failed")
		}
	}
	return nil
}

func refresh(ctx context.Context, resolver *dnsresolver.Resolver, activePolicy *policy.Active, state *state) error {
	interval := time.Duration(activePolicy.DNS().MinimumTTLSeconds) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := preflight(ctx, resolver, activePolicy); err != nil {
				state.setResolverReady(false)
				state.setProcess(processNotReady)
				continue
			}
			state.setResolverReady(true)
			state.setProcess(processReady)
		}
	}
}
