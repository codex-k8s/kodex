// Package app содержит единственный production composition root egress gateway.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"time"

	"github.com/codex-k8s/kodex/libs/go/httpserver"
	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/dnsresolver"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/gateway"
	internalobservability "github.com/codex-k8s/kodex/services/external/egress-gateway/internal/observability"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

const (
	metricsSubsystem       = "egress_gateway"
	technicalShutdown      = 5 * time.Second
	workerShutdown         = 5 * time.Second
	maximumShutdown        = 20 * time.Second
	terminationGraceMargin = 15 * time.Second
)

// MinimumTerminationGrace покрывает максимальный ordered shutdown и process-exit margin.
const MinimumTerminationGrace = maximumShutdown + workerShutdown + technicalShutdown + terminationGraceMargin

type runtime struct {
	state     *state
	technical *httpserver.Server
	connect   *gateway.Server
	workers   *serviceruntime.WorkerGroup
	policy    *policy.Active
	cancelRun context.CancelFunc
}

// Run загружает typed config и материализует startup/readiness/shutdown lifecycle.
func Run(lifecycle, shutdownBase context.Context, buildVersion string) error {
	config, err := loadConfig()
	if err != nil {
		return err
	}
	readiness := serviceruntime.NewReadiness()
	metrics := sharedobservability.NewMetrics(metricsSubsystem, buildVersion, map[string]string{})
	metrics.SetReady(false)
	business, err := internalobservability.New(metrics.Register)
	if err != nil {
		return fmt.Errorf("register egress gateway metrics: %w", err)
	}
	activePolicy, policyErr := policy.LoadFile(config.PolicyFile, config.ExpectedRevision, config.ExpectedDigest)
	if policyErr != nil {
		return runTechnicalOnly(lifecycle, shutdownBase, config, newInvalidPolicyState(readiness, metrics, business), metrics, business)
	}
	servers, err := dnsresolver.LoadSystemServers(config.ResolverConfig)
	if err != nil {
		return runTechnicalOnly(lifecycle, shutdownBase, config, newDegradedState(activePolicy, readiness, metrics, business), metrics, business)
	}
	return runActive(lifecycle, shutdownBase, config, activePolicy, servers, readiness, metrics, business)
}

func runActive(
	lifecycle, shutdownBase context.Context,
	config Config,
	activePolicy *policy.Active,
	servers []netip.AddrPort,
	readiness *serviceruntime.Readiness,
	metrics *sharedobservability.Metrics,
	business *internalobservability.Metrics,
) (resultErr error) {
	runContext, cancelRun := context.WithCancel(lifecycle)
	current := &runtime{policy: activePolicy, cancelRun: cancelRun}
	defer func() { resultErr = errors.Join(resultErr, current.shutdown(context.WithoutCancel(shutdownBase))) }()
	current.state = newState(activePolicy, readiness, metrics, business)
	resolver, err := dnsresolver.New(activePolicy.DNS(), servers, nil, func(outcome string, reason dnsresolver.Reason) {
		business.DNSObserver(outcome, string(reason))
		if outcome == "rejected" {
			current.state.setResolverReady(false)
			current.state.setProcess(processNotReady)
		}
	})
	if err != nil {
		return err
	}
	current.technical, err = newTechnicalServer(config.TechnicalAddress, current.state, metrics)
	if err != nil {
		return err
	}
	if err := current.technical.Listen(); err != nil {
		return err
	}
	technicalResult := make(chan error, 1)
	go func() { technicalResult <- current.technical.Serve() }()

	startupInterval := time.Duration(activePolicy.DNS().MinimumTTLSeconds) * time.Second
	for {
		if err := preflight(runContext, resolver, activePolicy); err == nil {
			current.state.setResolverReady(true)
			break
		}
		current.state.setProcess(processNotReady)
		timer := time.NewTimer(startupInterval)
		select {
		case <-lifecycle.Done():
			timer.Stop()
			return nil
		case serveErr := <-technicalResult:
			timer.Stop()
			return serveResult("technical HTTP", serveErr)
		case <-timer.C:
		}
	}

	current.connect, err = gateway.New(runContext, config.ConnectAddress, activePolicy, resolver, &gateway.NetDialer{}, current.state, business)
	if err != nil {
		return err
	}
	if err := current.connect.Listen(); err != nil {
		return err
	}
	connectResult := make(chan error, 1)
	go func() { connectResult <- current.connect.Serve() }()
	current.workers = serviceruntime.StartWorkers(runContext, refresh(activePolicy, resolver, current.state))
	workerResult := make(chan error, 1)
	go func() { workerResult <- current.workers.Wait(runContext) }()
	current.state.setProcess(processReady)

	select {
	case <-lifecycle.Done():
		return nil
	case serveErr := <-technicalResult:
		return serveResult("technical HTTP", serveErr)
	case serveErr := <-connectResult:
		return serveResult("CONNECT", serveErr)
	case workerErr := <-workerResult:
		return readinessWorkerResult(lifecycle, workerErr)
	}
}

func readinessWorkerResult(lifecycle context.Context, workerErr error) error {
	if lifecycleErr := lifecycle.Err(); lifecycleErr != nil &&
		(workerErr == nil || errors.Is(workerErr, lifecycleErr)) {
		return nil
	}
	if workerErr != nil {
		return fmt.Errorf("egress gateway readiness worker stopped: %w", workerErr)
	}
	return errors.New("egress gateway readiness worker stopped unexpectedly")
}

func runTechnicalOnly(
	lifecycle, shutdownBase context.Context,
	config Config,
	currentState *state,
	metrics *sharedobservability.Metrics,
	business *internalobservability.Metrics,
) (resultErr error) {
	technical, err := newTechnicalServer(config.TechnicalAddress, currentState, metrics)
	if err != nil {
		return err
	}
	if err := technical.Listen(); err != nil {
		return err
	}
	technicalResult := make(chan error, 1)
	go func() { technicalResult <- technical.Serve() }()
	compatibility, err := gateway.NewReadinessOnly(lifecycle, config.ConnectAddress, currentState, business)
	if err != nil {
		currentState.setProcess(processDraining)
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(shutdownBase), technicalShutdown)
		defer cancel()
		shutdownErr := technical.Shutdown(shutdownCtx)
		currentState.setProcess(processStopped)
		return errors.Join(err, shutdownErr)
	}
	if err := compatibility.Listen(); err != nil {
		currentState.setProcess(processDraining)
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(shutdownBase), technicalShutdown)
		defer cancel()
		shutdownErr := technical.Shutdown(shutdownCtx)
		currentState.setProcess(processStopped)
		return errors.Join(err, shutdownErr)
	}
	defer func() {
		currentState.setProcess(processDraining)
		shutdownErr := serviceruntime.RunShutdown(context.WithoutCancel(shutdownBase),
			serviceruntime.ShutdownOperation{Name: "compatibility readiness", Timeout: technicalShutdown, Run: compatibility.Shutdown},
			serviceruntime.ShutdownOperation{Name: "technical HTTP", Timeout: technicalShutdown, Run: technical.Shutdown},
		)
		currentState.setProcess(processStopped)
		resultErr = errors.Join(resultErr, shutdownErr)
	}()
	compatibilityResult := make(chan error, 1)
	go func() { compatibilityResult <- compatibility.Serve() }()
	select {
	case <-lifecycle.Done():
		return nil
	case serveErr := <-technicalResult:
		return serveResult("technical HTTP", serveErr)
	case serveErr := <-compatibilityResult:
		return serveResult("compatibility readiness", serveErr)
	}
}

func newTechnicalServer(address string, currentState *state, metrics *sharedobservability.Metrics) (*httpserver.Server, error) {
	return httpserver.New(httpserver.Config{
		Address: address, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second,
		MaximumHeaderBytes: 16 << 10, MaximumConnections: 64,
	}, currentState, metrics.PrometheusHandler(), httpserver.ExactGETRoute{
		Path: "/policy", ContentType: "application/json", Handler: newPolicyHandler(currentState),
	})
}

func newPolicyHandler(currentState *state) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(writer).Encode(currentState.readback())
	})
}

func (current *runtime) shutdown(base context.Context) error {
	if current.state == nil {
		return nil
	}
	current.state.setProcess(processDraining)
	current.cancelRun()
	if current.workers != nil {
		current.workers.Stop()
	}
	shutdownTimeout := maximumShutdown
	if current.policy != nil {
		shutdownTimeout = time.Duration(current.policy.Limits().ShutdownTimeoutMilliseconds) * time.Millisecond
	}
	result := serviceruntime.RunShutdown(base,
		serviceruntime.ShutdownOperation{Name: "CONNECT server", Timeout: shutdownTimeout, Run: func(ctx context.Context) error {
			if current.connect == nil {
				return nil
			}
			return current.connect.Shutdown(ctx)
		}},
		serviceruntime.ShutdownOperation{Name: "readiness worker", Timeout: workerShutdown, Run: func(ctx context.Context) error {
			if current.workers == nil {
				return nil
			}
			return current.workers.Wait(ctx)
		}},
		serviceruntime.ShutdownOperation{Name: "technical HTTP", Timeout: technicalShutdown, Run: func(ctx context.Context) error {
			if current.technical == nil {
				return nil
			}
			return current.technical.Shutdown(ctx)
		}},
	)
	current.state.setProcess(processStopped)
	return result
}

func preflight(ctx context.Context, resolver *dnsresolver.Resolver, activePolicy *policy.Active) error {
	for _, destination := range activePolicy.Destinations() {
		if _, err := resolver.Resolve(ctx, destination.Hostname); err != nil {
			return errors.New("DNS readiness preflight failed")
		}
	}
	return nil
}

func refresh(activePolicy *policy.Active, resolver *dnsresolver.Resolver, currentState *state) serviceruntime.Worker {
	return func(ctx context.Context) error {
		interval := time.Duration(activePolicy.DNS().MinimumTTLSeconds) * time.Second
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				if err := preflight(ctx, resolver, activePolicy); err != nil {
					currentState.setResolverReady(false)
					currentState.setProcess(processNotReady)
					continue
				}
				currentState.setResolverReady(true)
				currentState.setProcess(processReady)
			}
		}
	}
}

func serveResult(name string, err error) error {
	if err != nil {
		return fmt.Errorf("serve %s: %w", name, err)
	}
	return fmt.Errorf("%s server stopped unexpectedly", name)
}
