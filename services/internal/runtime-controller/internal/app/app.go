// Package app composes runtime-controller startup, workload workers, readiness and shutdown.
// Пакет app собирает запуск runtime-controller, работников нагрузок, готовность и завершение работы.
package app

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	sharedhttpserver "github.com/codex-k8s/kodex/libs/go/httpserver"
	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/internal/runtime-controller/internal/callback"
	"github.com/codex-k8s/kodex/services/internal/runtime-controller/internal/credentialprojection"
	"github.com/codex-k8s/kodex/services/internal/runtime-controller/internal/workload"
)

const (
	// serviceName provides the fixed telemetry identity for the runtime-controller process.
	// serviceName задаёт фиксированную идентичность процесса runtime-controller в телеметрии.
	serviceName = "runtime-controller"
	// issuerUID pins the expected local authority issuer's operating-system user for protected client construction.
	// It is a peer credential expectation, not the controller's own user ID.
	// issuerUID закрепляет ожидаемого системного пользователя локального издателя полномочий при создании защищённых клиентов.
	// Это ожидаемый реквизит узла, а не идентификатор пользователя самого контроллера.
	issuerUID = 29001
	// issuerGID pins the expected local authority issuer's operating-system group, independently of its transport address.
	// issuerGID закрепляет ожидаемую системную группу локального издателя полномочий отдельно от адреса транспорта.
	issuerGID = 29000
	// kubernetesLastKnownGoodWindow limits explicitly allowed degraded Kubernetes observation to two minutes after the last success.
	// It does not extend a lease, grant or execution deadline.
	// kubernetesLastKnownGoodWindow ограничивает явно разрешённое ухудшенное наблюдение Kubernetes двумя минутами после последнего успеха.
	// Аренда, разрешение и срок выполнения не продлеваются.
	kubernetesLastKnownGoodWindow = 2 * time.Minute
)

// kubernetesReadinessObserver tracks a caller-serialized last successful observation and degradation transition with an injectable clock.
// It does not classify errors or synchronize concurrent callers itself.
// kubernetesReadinessObserver хранит последнее успешное наблюдение и переход ухудшения под последовательным доступом потребителя, с заменяемыми часами.
// Классификация ошибок и синхронизация конкурирующих потребителей самим типом не выполняются.
type kubernetesReadinessObserver struct {
	now         func() time.Time
	lastSuccess time.Time
	degraded    bool
}

// newKubernetesReadinessObserver creates an empty observation history using the current wall clock.
// No Kubernetes probe is performed and no successful baseline is assumed.
// newKubernetesReadinessObserver создаёт пустую историю наблюдений с текущими часами.
// Запрос Kubernetes не выполняется, успешная исходная проверка не предполагается.
func newKubernetesReadinessObserver() *kubernetesReadinessObserver {
	return &kubernetesReadinessObserver{now: time.Now}
}

// Observe records successful checks and marks failure transitions, returning availability, a degradation-change flag and degraded state.
// A failed check can use the prior success only when the caller explicitly allows it and the exact last-known-good window has not elapsed.
// Observe сохраняет успешные проверки и переходы ошибок, возвращая доступность, признак смены ухудшения и состояние ухудшения.
// При ошибке прежний успех используется только с явным разрешением потребителя и до истечения точного окна последнего успешного наблюдения.
func (observer *kubernetesReadinessObserver) Observe(err error, allowLastKnownGood bool) (available, changed, degraded bool) {
	now := observer.now().UTC()
	if err == nil {
		changed = observer.degraded
		observer.lastSuccess = now
		observer.degraded = false
		return true, changed, false
	}
	changed = !observer.degraded
	observer.degraded = true
	if !allowLastKnownGood {
		return false, changed, true
	}
	available = !observer.lastSuccess.IsZero() && now.Before(observer.lastSuccess.Add(kubernetesLastKnownGoodWindow))
	return available, changed, true
}

// Run loads validated settings, composes protected clients, telemetry, workload management and callback handling, then starts workers after the Kubernetes startup check.
// It keeps callback draining separate from lifecycle cancellation, joins workers and collects shutdown errors before closing clients; tracing and Sentry receive separate budgets from shutdownBase.
// Run загружает проверенные настройки, собирает защищённые клиенты, телеметрию, управление нагрузками и обратные вызовы, затем запускает работников после начальной проверки Kubernetes.
// Ожидание обратных вызовов отделено от отмены жизненного цикла, работники завершаются и ошибки очистки собираются до закрытия клиентов; трассировка и Sentry получают раздельные бюджеты от shutdownBase.
func Run(lifecycle, shutdownBase context.Context, buildVersion string) (resultErr error) {
	config, err := loadConfig()
	if err != nil {
		return err
	}
	startup, cancelStartup := context.WithTimeout(lifecycle, 10*time.Second)
	defer cancelStartup()
	telemetry, err := sharedobservability.NewRuntimeFromEnv(startup, serviceName, buildVersion)
	if err != nil {
		return err
	}
	logger := telemetry.Logger(os.Stdout)
	metrics := sharedobservability.NewMetrics(serviceName, buildVersion, nil)
	defer func() {
		trace, cancelTrace := context.WithTimeout(shutdownBase, 5*time.Second)
		resultErr = errors.Join(resultErr, telemetry.ShutdownTracing(trace))
		cancelTrace()
		sentry, cancelSentry := context.WithTimeout(shutdownBase, 5*time.Second)
		resultErr = errors.Join(resultErr, telemetry.FlushSentry(sentry))
		cancelSentry()
	}()
	runtimeOperations := controlplaneclient.RuntimeOperations()
	proofOperations := mergeOperations(runtimeOperations, controlplaneclient.RuntimeCredentialProjectionOperations())
	projectionProjects := make(map[string]struct{})
	for operation := range controlplaneclient.RuntimeCredentialProjectionOperations() {
		if operation == "platform.runtime.credentials.materialize" {
			projectionProjects[operation] = struct{}{}
		}
	}
	control, err := controlplaneclient.Dial(startup, controlplaneclient.Config{ServiceIdentity: true,
		RPCProfile: config.RPCProfile, CallerWorkload: "runtime-controller",
		Target: config.ControlPlaneTarget, TLSServerName: config.ControlPlaneTLSServerName,
		CAFile: config.ControlPlaneCAFile, ClientCertificateFile: config.ControlPlaneCertificateFile,
		ClientPrivateKeyFile: config.ControlPlanePrivateKeyFile, ApplicationGrantFile: config.ApplicationGrantFile,
		ExpectedIssuerUID: issuerUID, ExpectedIssuerGID: issuerGID, DialTimeout: config.RequestTimeout,
		Operations: runtimeOperations, ProofOperations: proofOperations, ProjectRequiredOperations: projectionProjects,
	})
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, control.Close()) }()
	credentials, err := credentialprojection.Dial(startup, credentialprojection.Config{
		RPCProfile: config.RPCProfile,
		Target:     config.SecretBrokerTarget, TLSServerName: config.SecretBrokerTLSServerName,
		CAFile: config.SecretBrokerCAFile, CertificateFile: config.ControlPlaneCertificateFile,
		PrivateKeyFile: config.ControlPlanePrivateKeyFile, ExpectedIssuerUID: issuerUID, ExpectedIssuerGID: issuerGID,
		DialTimeout: config.RequestTimeout, Proofs: control,
	})
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, credentials.Close()) }()
	egressSigningKey, err := securefile.Read(config.RuntimeEgressSigningKeyFile, 4096)
	if err != nil {
		return errors.New("read runtime egress signing key")
	}
	manager, err := workload.InCluster(workload.Config{
		RPCProfile:  config.RPCProfile,
		Environment: config.Environment, ControlNamespace: config.ControlNamespace, RuntimeNamespace: config.RuntimeNamespace,
		ControllerPodUID: config.PodUID, ControllerPodIP: config.PodIP,
		CallbackTLSServerName: config.CallbackTLSServerName, CallbackClientCASecret: config.CallbackClientCASecret,
		CallbackClientTLSSecret: config.CallbackClientTLSSecret, ProviderHTTPSProxy: config.ProviderHTTPSProxy,
		RuntimeEgressSigningKey: egressSigningKey,
		RuntimeEgressCASecret:   config.RuntimeEgressCASecret,
		ProviderAppArmorProfile: config.ProviderAppArmorProfile,
		StorageClass:            config.StorageClass, SessionPVCSize: config.SessionPVCSize, RunnerServiceAccount: config.RunnerServiceAccount,
		PromotedRoleImageRepository: config.PromotedRoleImageRepository, RoleRuntimeContractRevision: config.RoleRuntimeContractRevision,
		DefaultRoleImageReference: config.DefaultRoleImageReference,
		RoleRuntimeContractSHA256: config.RoleRuntimeContractSHA256,
	})
	if err != nil {
		return err
	}
	if err := manager.Check(startup); err != nil {
		return errors.Join(errors.New("runtime controller startup barrier failed"), err)
	}
	coordinator := callback.NewCoordinator()
	callbackServer, err := callback.New(callback.Config{Listen: config.CallbackListen,
		RPCProfile:      config.RPCProfile,
		CertificateFile: config.CallbackServerCertificateFile, PrivateKeyFile: config.CallbackServerPrivateKeyFile,
		ClientCAFile: config.CallbackClientCAFile, ExpectedClientSPIFFEID: config.CallbackExpectedClientSPIFFEID,
		RequestTimeout: config.RequestTimeout, WarmLongPoll: config.WarmLongPoll,
		FileTransferTimeout: config.FileTransferTimeout, ArtifactSpoolDirectory: config.ArtifactSpoolDirectory}, manager, control, coordinator, logger)
	if err != nil {
		return err
	}
	unitReadiness, assistantReadiness := serviceruntime.NewReadiness(), serviceruntime.NewReadiness()
	defer func() { resultErr = errors.Join(resultErr, callbackServer.Close()) }()
	unitReadiness.Set(false, "infrastructure_starting")
	metrics.SetReady(false)
	assistantReadiness.Set(false, "assistant_runtime_starting")
	runtime := newRuntime(control.Runtime, credentials, manager, coordinator, config, assistantReadiness, logger)
	technical := technicalServer(lifecycle, config, unitReadiness, assistantReadiness, metrics)
	runtimeDone := make(chan struct{})
	workers := serviceruntime.StartWorkers(lifecycle,
		sharedhttpserver.ShutdownWorker(technical.ListenAndServe, technical.Shutdown, config.ShutdownTimeout),
		func(ctx context.Context) error {
			return runCallbackDuringDrain(ctx, runtimeDone, workload.ControllerShutdownDrain+5*time.Second, callbackServer.Run)
		},
		monitorUnitReadiness(manager, callbackServer, unitReadiness, metrics, logger, config),
		func(ctx context.Context) error {
			defer close(runtimeDone)
			return manager.RunAsLeader(ctx, runtime.Run)
		},
	)
	err = workers.Wait(context.WithoutCancel(lifecycle))
	shutdownErr := serviceruntime.RunShutdown(shutdownBase,
		serviceruntime.ShutdownOperation{Name: "technical HTTP server", Timeout: config.ShutdownTimeout, Run: technical.Shutdown},
		serviceruntime.ShutdownOperation{Name: "runtime callback server", Timeout: config.ShutdownTimeout, Run: callbackServer.Shutdown},
		serviceruntime.ShutdownOperation{Name: "runtime workers", Timeout: config.ShutdownTimeout, Run: workers.Wait},
	)
	return errors.Join(err, shutdownErr)
}

// runCallbackDuringDrain runs callbacks with lifecycle values but without its cancellation/deadline, and joins a watcher on return.
// After lifecycle cancellation, callbacks are canceled only when runtime drain completes or the supplied maximum elapses; early callback return also releases the watcher.
// runCallbackDuringDrain выполняет обратные вызовы со значениями жизненного цикла, но без его отмены/срока, и при выходе дожидается наблюдателя.
// После отмены жизненного цикла обратные вызовы отменяются по завершении ожидания выполнения либо истечении переданного максимума; ранний возврат также освобождает наблюдателя.
func runCallbackDuringDrain(ctx context.Context, runtimeDone <-chan struct{}, maximum time.Duration, run func(context.Context) error) error {
	callbackContext, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	finished, watcherDone := make(chan struct{}), make(chan struct{})
	defer func() { close(finished); <-watcherDone }()
	go func() {
		defer close(watcherDone)
		select {
		case <-finished:
			return
		case <-ctx.Done():
		}
		timer := time.NewTimer(maximum)
		defer timer.Stop()
		select {
		case <-finished:
			return
		case <-runtimeDone:
		case <-timer.C:
		}
		cancel()
	}()
	return run(callbackContext)
}

// mergeOperations copies operation-to-method entries into a fresh map, with later sets replacing earlier duplicate keys.
// It neither mutates the input registries nor issues or validates application authority.
// mergeOperations копирует соответствия операций и методов в новую карту, заменяя совпадающие ключи значениями последующих наборов.
// Исходные реестры не меняются; прикладные полномочия не выдаются и не проверяются.
func mergeOperations(sets ...map[string]string) map[string]string {
	result := make(map[string]string)
	for _, set := range sets {
		for operation, method := range set {
			result[operation] = method
		}
	}
	return result
}

// technicalServer constructs the existing health, unit/assistant readiness and metrics routes with explicit HTTP limits and lifecycle base context.
// Construction opens no listener; the handlers preserve their current method behavior and do not perform readiness probes on each request.
// technicalServer создаёт существующие маршруты жизни, готовности сервиса/помощника и метрик с явными лимитами HTTP и базовым контекстом жизненного цикла.
// При создании listener не открывается; обработчики сохраняют текущую работу с методами и не выполняют проверки инфраструктуры на каждый запрос.
func technicalServer(lifecycle context.Context, config Config, unit, assistant *serviceruntime.Readiness, metrics *sharedobservability.Metrics) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/livez", func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/readyz", readinessHandler(unit))
	mux.HandleFunc("/assistant/readyz", readinessHandler(assistant))
	mux.Handle("/metrics", metrics.PrometheusHandler())
	return &http.Server{Addr: config.TechnicalListen, Handler: mux, BaseContext: func(net.Listener) context.Context { return lifecycle },
		ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
}

// readinessHandler reads only the locally maintained readiness snapshot, returning 204 when ready or 503 with its reason otherwise.
// It performs no Kubernetes, broker, filesystem or downstream RPC checks.
// readinessHandler читает только локально поддерживаемый снимок готовности, возвращая 204 при готовности либо 503 с причиной.
// Проверки Kubernetes, брокера, файловой системы и нижележащих RPC не выполняются.
func readinessHandler(readiness *serviceruntime.Readiness) http.HandlerFunc {
	return func(writer http.ResponseWriter, _ *http.Request) {
		ready, reason := readiness.Ready()
		if !ready {
			http.Error(writer, reason, http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}
}

// monitorUnitReadiness returns a worker that checks artifact spool and Kubernetes with separate request budgets, immediately and then on the configured interval.
// Only explicitly eligible Kubernetes failures may use bounded last-known-good availability; readiness/metrics and transition logs remain local, and cancellation ends the worker.
// monitorUnitReadiness возвращает работника, проверяющего хранилище вложений и Kubernetes с раздельными бюджетами запроса, сразу и затем с настроенным интервалом.
// Только явно допустимые ошибки Kubernetes могут использовать ограниченную доступность последнего успеха; готовность/метрики и логи переходов остаются локальными, отмена завершает работника.
func monitorUnitReadiness(manager *workload.Manager, callbacks *callback.Server, readiness *serviceruntime.Readiness, metrics *sharedobservability.Metrics, logger *slog.Logger, config Config) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(config.InfrastructureCheckInterval)
		defer ticker.Stop()
		kubernetes := newKubernetesReadinessObserver()
		for {
			spoolCheck, cancelSpool := context.WithTimeout(ctx, config.RequestTimeout)
			spoolErr := callbacks.CheckArtifactSpool(spoolCheck)
			cancelSpool()
			kubernetesCheck, cancelKubernetes := context.WithTimeout(ctx, config.RequestTimeout)
			kubernetesErr := manager.Check(kubernetesCheck)
			cancelKubernetes()
			kubernetesAvailable, kubernetesChanged, kubernetesDegraded := kubernetes.Observe(kubernetesErr, workload.AllowsLastKnownGoodObservation(kubernetesErr))
			if kubernetesChanged {
				if kubernetesDegraded {
					logger.WarnContext(ctx, "Kubernetes runtime observation degraded", "error_class", "kubernetes")
				} else {
					logger.InfoContext(ctx, "Kubernetes runtime observation restored")
				}
			}
			if spoolErr == nil && kubernetesAvailable {
				metrics.SetReady(true)
				if readiness.Set(true, "ready") {
					logger.InfoContext(ctx, "runtime readiness restored")
				}
			} else if readiness.Set(false, "local_infrastructure_unavailable") {
				metrics.SetReady(false)
				class := "kubernetes"
				if spoolErr != nil {
					class = "artifact_spool"
				}
				logger.WarnContext(ctx, "runtime readiness lost", "error_class", class)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}
}
