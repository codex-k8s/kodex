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
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/mailpolicy"
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
	connects  []*gateway.Server
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
	if _, err := activePolicy.ForProfile(policy.STTProfileName); err != nil {
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

	sttPolicy, err := activePolicy.ForProfile(policy.STTProfileName)
	if err != nil {
		return err
	}
	for index, profile := range []*policy.Active{activePolicy, sttPolicy} {
		address := config.ConnectAddress
		if index == 1 {
			address = config.STTConnectAddress
		}
		server, err := gateway.New(runContext, address, profile, resolver, &gateway.NetDialer{}, current.state, business)
		if err != nil {
			return err
		}
		current.connects = append(current.connects, server)
	}
	mailResolver, err := dnsresolver.New(activePolicy.DNS(), servers, nil, func(outcome string, reason dnsresolver.Reason) {
		business.DNSObserver(outcome, string(reason))
	})
	if err != nil {
		return err
	}
	mailActive, mailErr := mailpolicy.LoadMailFile(config.MailPolicyFile, config.MailExpectedDigest, activePolicy)
	mailReadiness := mailpolicy.NewReadiness(mailActive, mailResolver)
	if err := internalobservability.RegisterMailReadiness(metrics.Register, mailReadiness.Ready); err != nil {
		return err
	}
	var mailServer *gateway.Server
	if mailErr != nil {
		mailServer, err = gateway.NewReadinessOnly(runContext, config.MailConnectAddress, mailReadiness, business)
	} else {
		mailServer, err = gateway.New(runContext, config.MailConnectAddress, mailActive, mailResolver, &gateway.NetDialer{}, mailReadiness, business)
	}
	if err != nil {
		return err
	}
	current.connects = append(current.connects, mailServer)
	for _, server := range current.connects[1:] {
		if err := server.ShareConnectionLimit(current.connects[0]); err != nil {
			return err
		}
	}
	connectResult := make(chan error, len(current.connects))
	for _, server := range current.connects {
		if err := server.Listen(); err != nil {
			return err
		}
		go func() { connectResult <- server.Serve() }()
	}
	current.workers = serviceruntime.StartWorkers(runContext, refresh(activePolicy, resolver, current.state), mailReadiness.Run(startupInterval))
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
	runContext, cancelRun := context.WithCancel(lifecycle)
	current := &runtime{state: currentState, cancelRun: cancelRun}
	defer func() { resultErr = errors.Join(resultErr, current.shutdown(context.WithoutCancel(shutdownBase))) }()
	technical, err := newTechnicalServer(config.TechnicalAddress, currentState, metrics)
	if err != nil {
		return err
	}
	current.technical = technical
	if err := technical.Listen(); err != nil {
		return err
	}
	technicalResult := make(chan error, 1)
	go func() { technicalResult <- technical.Serve() }()
	for _, address := range []string{config.ConnectAddress, config.STTConnectAddress, config.MailConnectAddress} {
		compatibility, err := gateway.NewReadinessOnly(runContext, address, currentState, business)
		if err != nil {
			return err
		}
		current.connects = append(current.connects, compatibility)
	}
	for _, server := range current.connects[1:] {
		if err := server.ShareConnectionLimit(current.connects[0]); err != nil {
			return err
		}
	}
	compatibilityResult := make(chan error, len(current.connects))
	for _, server := range current.connects {
		if err := server.Listen(); err != nil {
			return err
		}
		go func() { compatibilityResult <- server.Serve() }()
	}
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
	for _, server := range current.connects {
		server.Drain()
	}
	if current.workers != nil {
		current.workers.Stop()
	}
	shutdownTimeout := maximumShutdown
	if current.policy != nil {
		shutdownTimeout = time.Duration(current.policy.Limits().ShutdownTimeoutMilliseconds) * time.Millisecond
	}
	result := serviceruntime.RunShutdown(base,
		serviceruntime.ShutdownOperation{Name: "CONNECT server", Timeout: shutdownTimeout, Run: func(ctx context.Context) error {
			var result error
			for _, server := range current.connects {
				result = errors.Join(result, server.Shutdown(ctx))
			}
			return result
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
