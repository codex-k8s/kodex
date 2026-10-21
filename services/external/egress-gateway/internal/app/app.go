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

	"github.com/codex-k8s/kodex/libs/go/dnsresolver"
	"github.com/codex-k8s/kodex/libs/go/httpserver"
	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/gateway"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/integrationpolicy"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/mailpolicy"
	internalobservability "github.com/codex-k8s/kodex/services/external/egress-gateway/internal/observability"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/runtimepolicy"
)

// Shutdown constants bound each phase and preserve a termination grace margin.
// Константы остановки ограничивают каждую фазу и оставляют запас на завершение.
const (
	metricsSubsystem       = "egress_gateway"
	technicalShutdown      = 5 * time.Second
	workerShutdown         = 5 * time.Second
	maximumShutdown        = 20 * time.Second
	terminationGraceMargin = 15 * time.Second
)

// MinimumTerminationGrace covers ordered shutdown phases and process-exit margin.
// MinimumTerminationGrace покрывает фазы упорядоченной остановки и запас на выход процесса.
const MinimumTerminationGrace = maximumShutdown + workerShutdown + technicalShutdown + terminationGraceMargin

// runtime owns listeners, workers, policy, and cancellation for one process run.
// runtime владеет слушателями, worker, политикой и отменой одного запуска.
type runtime struct {
	state     *state
	technical *httpserver.Server
	connects  []*gateway.Server
	workers   *serviceruntime.WorkerGroup
	policy    *policy.Active
	cancelRun context.CancelFunc
}

// Run loads typed configuration and owns the startup, readiness, and shutdown lifecycle.
// Run загружает типизированную конфигурацию и ведёт запуск, готовность и остановку.
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
	if config.Mode == "clamav" {
		destinations := activePolicy.Destinations()
		if len(destinations) != 1 || destinations[0] != (policy.Destination{Hostname: "database.clamav.net", Port: 443}) {
			return runTechnicalOnly(lifecycle, shutdownBase, config, newInvalidPolicyState(readiness, metrics, business), metrics, business)
		}
	}
	if config.Mode != "clamav" {
		if _, err := activePolicy.ForProfile(policy.STTProfileName); err != nil {
			return runTechnicalOnly(lifecycle, shutdownBase, config, newInvalidPolicyState(readiness, metrics, business), metrics, business)
		}
	}
	servers, err := dnsresolver.LoadSystemServers(config.ResolverConfig)
	if err != nil {
		return runTechnicalOnly(lifecycle, shutdownBase, config, newDegradedState(activePolicy, readiness, metrics, business), metrics, business)
	}
	if config.Mode == "clamav" {
		return runSingleDestination(lifecycle, shutdownBase, config, activePolicy, servers, readiness, metrics, business)
	}
	return runActive(lifecycle, shutdownBase, config, activePolicy, servers, readiness, metrics, business)
}

// runSingleDestination serves only the signed scanner database destination.
// runSingleDestination обслуживает только подписанный источник баз сканера;
// прочие listeners и динамические policy этой нагрузке не принадлежат.
func runSingleDestination(
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
	resolver, technicalResult, err := current.startPolicy(activePolicy, servers, config.TechnicalAddress, readiness, metrics, business)
	if err != nil {
		return err
	}
	connect, err := gateway.New(runContext, config.ConnectAddress, activePolicy, resolver, &gateway.NetDialer{}, current.state, business)
	if err != nil {
		return err
	}
	current.connects = []*gateway.Server{connect}
	connectResult, err := current.startConnects()
	if err != nil {
		return err
	}
	current.state.setProcess(processReady)
	select {
	case <-lifecycle.Done():
		return nil
	case serveErr := <-technicalResult:
		return serveResult("technical HTTP", serveErr)
	case serveErr := <-connectResult:
		return serveResult("CONNECT", serveErr)
	}
}

// runActive starts each authorized egress listener and its readiness workers.
// runActive запускает разрешённые egress-слушатели и workers готовности.
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
	resolver, technicalResult, err := current.startPolicy(activePolicy, servers, config.TechnicalAddress, readiness, metrics, business)
	if err != nil {
		return err
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
	runtimeKey, err := securefile.Read(config.RuntimeSigningKeyFile, 4096)
	if err != nil {
		return errors.New("read runtime egress signing key")
	}
	runtimePolicy, err := runtimepolicy.New(activePolicy, runtimeKey)
	if err != nil {
		return err
	}
	runtimeProxyCACertificate, err := securefile.Read(config.RuntimeProxyCACertificate, 64<<10)
	if err != nil {
		return errors.New("read runtime proxy CA certificate")
	}
	runtimeProxyCAPrivateKey, err := securefile.Read(config.RuntimeProxyCAPrivateKey, 64<<10)
	if err != nil {
		return errors.New("read runtime proxy CA private key")
	}
	runtimeProxyCA, err := gateway.NewTLSInterceptAuthority(runtimeProxyCACertificate, runtimeProxyCAPrivateKey)
	if err != nil {
		return err
	}
	runtimeServer, err := gateway.NewAuthenticated(runContext, config.RuntimeConnectAddress, runtimePolicy, resolver, &gateway.NetDialer{}, current.state, business, runtimeProxyCA)
	if err != nil {
		return err
	}
	current.connects = append(current.connects, runtimeServer)
	mailResolver, err := newObservedResolver(activePolicy, servers, business)
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
	integrationResolver, err := newObservedResolver(activePolicy, servers, business)
	if err != nil {
		return err
	}
	integrationActive, integrationErr := integrationpolicy.LoadFile(config.IntegrationPolicyFile, config.IntegrationExpectedDigest, activePolicy)
	integrationReadiness := integrationpolicy.NewReadiness(integrationActive, integrationResolver)
	if err := internalobservability.RegisterIntegrationReadiness(metrics.Register, integrationReadiness.Ready); err != nil {
		return err
	}
	var integrationServer *gateway.Server
	if integrationErr != nil {
		integrationServer, err = gateway.NewReadinessOnly(runContext, config.IntegrationConnectAddress, integrationReadiness, business)
	} else {
		integrationServer, err = gateway.New(runContext, config.IntegrationConnectAddress, integrationActive, integrationResolver, &gateway.NetDialer{}, integrationReadiness, business)
	}
	if err != nil {
		return err
	}
	current.connects = append(current.connects, integrationServer)
	connectResult, err := current.startConnects()
	if err != nil {
		return err
	}
	mailRefreshInterval := time.Duration(activePolicy.DNS().MinimumTTLSeconds) * time.Second
	current.workers = serviceruntime.StartWorkers(runContext, mailReadiness.Run(mailRefreshInterval), integrationReadiness.Run(mailRefreshInterval))
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

// readinessWorkerResult distinguishes requested shutdown from an unexpected worker stop.
// readinessWorkerResult отличает запрошенную остановку от сбоя worker готовности.
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

// runTechnicalOnly keeps readiness endpoints available while policy or DNS is invalid.
// runTechnicalOnly сохраняет endpoints готовности при ошибке политики или DNS.
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
	technicalResult, err := current.startTechnical(config.TechnicalAddress, metrics)
	if err != nil {
		return err
	}
	addresses := []string{config.ConnectAddress}
	if config.Mode != "clamav" {
		addresses = append(addresses, config.STTConnectAddress, config.MailConnectAddress, config.IntegrationConnectAddress)
	}
	for _, address := range addresses {
		compatibility, err := gateway.NewReadinessOnly(runContext, address, currentState, business)
		if err != nil {
			return err
		}
		current.connects = append(current.connects, compatibility)
	}
	compatibilityResult, err := current.startConnects()
	if err != nil {
		return err
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

// newObservedResolver creates a separate resolver with the shared DNS observer.
// newObservedResolver создаёт отдельный resolver с общей метрикой DNS.
func newObservedResolver(activePolicy *policy.Active, servers []netip.AddrPort, business *internalobservability.Metrics) (*dnsresolver.Resolver, error) {
	return dnsresolver.New(activePolicy.DNS(), servers, nil, func(outcome string, reason dnsresolver.Reason) {
		business.DNSObserver(outcome, string(reason))
	})
}

// startPolicy initializes active state, DNS, and technical HTTP in startup order.
// startPolicy по порядку настраивает активное состояние, DNS и технический HTTP.
func (current *runtime) startPolicy(
	activePolicy *policy.Active,
	servers []netip.AddrPort,
	address string,
	readiness *serviceruntime.Readiness,
	metrics *sharedobservability.Metrics,
	business *internalobservability.Metrics,
) (*dnsresolver.Resolver, <-chan error, error) {
	current.state = newState(activePolicy, readiness, metrics, business)
	resolver, err := newObservedResolver(activePolicy, servers, business)
	if err != nil {
		return nil, nil, err
	}
	current.state.setResolverConfigured()
	technicalResult, err := current.startTechnical(address, metrics)
	if err != nil {
		return nil, nil, err
	}
	return resolver, technicalResult, nil
}

// startTechnical reserves and serves the technical listener before readiness.
// startTechnical резервирует и запускает технический listener до готовности.
func (current *runtime) startTechnical(address string, metrics *sharedobservability.Metrics) (<-chan error, error) {
	technical, err := newTechnicalServer(address, current.state, metrics)
	if err != nil {
		return nil, err
	}
	current.technical = technical
	if err := technical.Listen(); err != nil {
		return nil, err
	}
	result := make(chan error, 1)
	go func() { result <- technical.Serve() }()
	return result, nil
}

// startConnects shares limits, reserves every CONNECT listener, then serves them.
// startConnects разделяет лимит, резервирует все CONNECT-слушатели и запускает их.
func (current *runtime) startConnects() (<-chan error, error) {
	for _, server := range current.connects[1:] {
		if err := server.ShareConnectionLimit(current.connects[0]); err != nil {
			return nil, err
		}
	}
	result := make(chan error, len(current.connects))
	for _, server := range current.connects {
		if err := server.Listen(); err != nil {
			return nil, err
		}
		go func() { result <- server.Serve() }()
	}
	return result, nil
}

// newTechnicalServer configures bounded health, metrics, and policy-readback routes.
// newTechnicalServer настраивает ограниченные маршруты здоровья, метрик и состояния policy.
func newTechnicalServer(address string, currentState *state, metrics *sharedobservability.Metrics) (*httpserver.Server, error) {
	return httpserver.New(httpserver.Config{
		Address: address, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second,
		MaximumHeaderBytes: 16 << 10, MaximumConnections: 64,
	}, currentState, metrics.PrometheusHandler(), httpserver.ExactGETRoute{
		Path: "/policy", ContentType: "application/json", Handler: newPolicyHandler(currentState),
	})
}

// newPolicyHandler exposes only the synchronized, non-secret policy readback.
// newPolicyHandler выдаёт только синхронизированное состояние policy без секретов.
func newPolicyHandler(currentState *state) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(writer).Encode(currentState.readback())
	})
}

// shutdown drains listeners before joining workers and closing technical HTTP.
// shutdown сначала осушает слушатели, затем дожидается workers и закрывает HTTP.
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
			results := make(chan error, len(current.connects))
			for _, server := range current.connects {
				go func() { results <- server.Shutdown(ctx) }()
			}
			var result error
			for range current.connects {
				result = errors.Join(result, <-results)
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

// serveResult treats an unrequested listener exit as a process failure.
// serveResult считает незапрошенный выход listener ошибкой процесса.
func serveResult(name string, err error) error {
	if err != nil {
		return fmt.Errorf("serve %s: %w", name, err)
	}
	return fmt.Errorf("%s server stopped unexpectedly", name)
}
