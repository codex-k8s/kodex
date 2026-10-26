// Package app composes protected integration work, technical HTTP and process-owned startup, polling and shutdown.
// RU: Пакет app собирает защищённую работу интеграций, технический HTTP и управляемые процессом запуск, опрос и остановку.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/httpserver"
	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
	businessmetrics "github.com/codex-k8s/kodex/services/external/integration-gateway/internal/observability/metrics"
	"github.com/google/uuid"
)

const (
	// issuerUID pins the expected owner of projected application-grant files; it does not identify the invoking user.
	// RU: issuerUID закрепляет ожидаемого владельца файлов application grant, а не идентичность вызывающего пользователя.
	issuerUID = 29001
	// issuerGID pins the expected issuer group checked by the protected control-plane client.
	// RU: issuerGID закрепляет ожидаемую группу издателя, проверяемую защищённым клиентом control plane.
	issuerGID = 29000
)

// Run validates configuration and initializes telemetry, protected owner RPC, the adapter and the technical listener before polling.
// RU: Run проверяет конфигурацию и создаёт телеметрию, защищённый owner RPC, адаптер и технический listener до начала опроса.
// Once workers have started, it clears readiness on termination and performs ordered bounded cleanup using the independent shutdown base.
// RU: После запуска workers функция снимает готовность при завершении и выполняет упорядоченную ограниченную очистку с независимой базой shutdown.
func Run(lifecycle, shutdownBase context.Context, buildVersion string) (resultErr error) {
	config, err := loadConfig()
	if err != nil {
		return err
	}
	startup, cancelStartup := context.WithTimeout(lifecycle, config.StartupTimeout)
	defer cancelStartup()
	telemetry, err := sharedobservability.NewRuntimeFromEnv(startup, serviceName, buildVersion)
	if err != nil {
		return err
	}
	logger := telemetry.Logger(os.Stdout)
	metrics := sharedobservability.NewMetrics(metricsSubsystem, buildVersion, map[string]string{})
	business, err := businessmetrics.New(metrics)
	if err != nil {
		return err
	}
	readiness := serviceruntime.NewReadiness()
	control, err := controlplaneclient.Dial(startup, controlplaneclient.Config{ServiceIdentity: true,
		RPCProfile: config.RPCProfile, CallerWorkload: serviceName,
		Target: config.ControlPlaneTarget, TLSServerName: config.ControlPlaneTLSServerName, CAFile: config.ControlPlaneCAFile,
		ClientCertificateFile: config.ControlPlaneCertificateFile, ClientPrivateKeyFile: config.ControlPlanePrivateKeyFile,
		ApplicationGrantFile: config.ApplicationGrantFile, ExpectedIssuerUID: issuerUID, ExpectedIssuerGID: issuerGID,
		DialTimeout: config.RequestTimeout, Operations: controlplaneclient.IntegrationGatewayOperations(),
	})
	if err != nil {
		return err
	}
	adapter, err := integration.New(integration.Config{
		RPCProfile:          config.RPCProfile,
		CredentialDirectory: config.CredentialDirectory, ProxyURL: config.EgressProxyURL, OpenAPIProxyURL: config.OpenAPIProxyURL,
		SyntheticBaseURL: config.SyntheticBaseURL, LocalOpenAPIBaseURL: config.LocalOpenAPIBaseURL,
		LocalOpenAPICAFile: config.LocalOpenAPICAFile, Timeout: config.OperationTimeout,
		EmailCAFile: config.ControlPlaneCAFile, EmailCertificateFile: config.ControlPlaneCertificateFile, EmailPrivateKeyFile: config.ControlPlanePrivateKeyFile,
	})
	if err != nil {
		return err
	}
	technical, err := httpserver.New(httpserver.StandardProcessConfig(config.TechnicalListen), readiness, metrics.PrometheusHandler())
	if err != nil {
		return err
	}
	if err := technical.Listen(); err != nil {
		return err
	}
	readiness.Set(true, "ready")
	metrics.SetReady(true)
	workHealth := &workCycleHealth{}
	workers := serviceruntime.StartWorkers(lifecycle, technical.Worker(), monitorWorkPathHealth(workHealth, business, config), runIntegrationLoop(control, adapter, workHealth, business, logger, config))
	err = workers.Wait(context.WithoutCancel(lifecycle))
	readiness.Set(false, "stopping")
	metrics.SetReady(false)
	workers.Stop()
	shutdownErr := serviceruntime.RunShutdown(shutdownBase,
		serviceruntime.ShutdownOperation{Name: "integration workers", Timeout: config.ShutdownTimeout / 2, Run: workers.Wait},
		serviceruntime.ShutdownOperation{Name: "control-plane client", Timeout: config.ShutdownTimeout / 4, Run: func(context.Context) error { return control.Close() }},
		serviceruntime.ShutdownOperation{Name: "technical HTTP", Timeout: config.ShutdownTimeout / 4, Run: technical.Shutdown},
		serviceruntime.ShutdownOperation{Name: "tracing", Timeout: 5 * time.Second, Run: telemetry.ShutdownTracing},
		serviceruntime.ShutdownOperation{Name: "error reporting", Timeout: 5 * time.Second, Run: telemetry.FlushSentry},
	)
	return errors.Join(err, shutdownErr)
}

// monitorWorkPathHealth periodically publishes freshness of completed owner work cycles to business metrics, not HTTP readiness.
// RU: monitorWorkPathHealth периодически публикует свежесть завершённых owner-циклов в бизнесовые метрики, не меняя HTTP readiness.
// Its tolerance includes the full cycle budget, readiness interval and five seconds; cancellation stops the ticker and worker.
// RU: Допуск включает полный бюджет цикла, интервал проверки и пять секунд; отмена останавливает ticker и worker.
func monitorWorkPathHealth(workHealth *workCycleHealth, business *businessmetrics.Metrics, config Config) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(config.ReadinessInterval)
		defer ticker.Stop()
		for {
			business.WorkPathReady(workHealth.ready(time.Now(), integrationCycleBudget(config)+5*time.Second+config.ReadinessInterval))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}
}

// runIntegrationLoop runs bounded sequential claim cycles, records health and metrics, and logs only degradation or recovery transitions.
// RU: runIntegrationLoop выполняет ограниченные последовательные claim-циклы, учитывает здоровье и метрики, журналирует только деградацию и восстановление.
// Productive successful cycles reset idle backoff; errors or idle cycles increase it, while cancellation interrupts the wait.
// RU: Успешный цикл с работой сбрасывает паузу простоя; ошибка или отсутствие работы увеличивают её, а отмена прерывает ожидание.
func runIntegrationLoop(control *controlplaneclient.Client, adapter *integration.Adapter, workHealth *workCycleHealth, metrics *businessmetrics.Metrics, logger *slog.Logger, config Config) serviceruntime.Worker {
	return func(ctx context.Context) error {
		idleBackoff := serviceruntime.NewIdleBackoff(config.PollInterval, 5*time.Second)
		degraded := false
		for {
			cycle, cancel := context.WithTimeout(ctx, integrationCycleBudget(config))
			processed, err := processIntegrationWork(cycle, control, adapter, metrics, logger, config)
			cancel()
			workHealth.record(time.Now(), err)
			if err != nil {
				metrics.WorkPathReady(false)
			}
			metrics.Cycle(err)
			if err != nil && !degraded {
				degraded = true
				logger.WarnContext(ctx, "integration work delivery degraded", "error_class", "control_plane_or_adapter")
			} else if err == nil && degraded {
				degraded = false
				logger.InfoContext(ctx, "integration work delivery restored")
			}
			timer := time.NewTimer(idleBackoff.Next(err == nil && processed > 0))
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
}

// processIntegrationWork processes connection tests, invocations, source reads and writebacks in that order, returning completed work even on a later error.
// RU: processIntegrationWork последовательно обрабатывает проверки подключений, вызовы, чтение источников и запись, сохраняя счётчик завершённого при последующей ошибке.
// Invocation mode selects read-only recovery or execution; insufficient remaining lease time prevents either provider call.
// RU: Режим вызова выбирает read-only восстановление либо исполнение; нехватка остатка аренды запрещает оба обращения к провайдеру.
// External-operation metrics are recorded before owner completion, and an unknown provider outcome remains explicit rather than retried here.
// RU: Метрики внешней операции учитываются до owner completion; неопределённый результат сохраняется явно и здесь не повторяется.
func processIntegrationWork(ctx context.Context, control *controlplaneclient.Client, adapter *integration.Adapter, metrics *businessmetrics.Metrics, logger *slog.Logger, config Config) (int, error) {
	tests, err := control.Runtime.ClaimIntegrationConnectionTests(ctx, &controlplanev1.ClaimIntegrationConnectionTestsRequest{WorkloadInstance: config.InstanceID, Limit: config.ClaimLimit})
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, claim := range tests.GetClaims() {
		operation, cancel := context.WithTimeout(ctx, config.OperationTimeout)
		result, operationErr := adapter.Test(operation, integration.RequestFromTest(claim))
		cancel()
		metrics.Operation(true, operationErr == nil, false)
		if err := completeTest(ctx, control, claim, result, operationErr); err != nil {
			return processed, err
		}
		processed++
	}
	invocations, err := control.Runtime.ClaimIntegrationInvocations(ctx, &controlplanev1.ClaimIntegrationInvocationsRequest{WorkloadInstance: config.InstanceID, Limit: config.ClaimLimit})
	if err != nil {
		return processed, err
	}
	for _, claim := range invocations.GetClaims() {
		if claim.GetLease().GetExpiresAt().AsTime().Before(time.Now().Add(config.OperationTimeout + config.RequestTimeout)) {
			return processed, errors.New("integration lease budget is insufficient")
		}
		operation, cancel := context.WithTimeout(ctx, config.OperationTimeout)
		var result integration.Result
		var operationErr error
		if claim.GetWorkMode() == controlplanev1.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_RECOVER_READ_ONLY {
			result, operationErr = adapter.Recover(operation, integration.RequestFromInvocation(claim))
		} else {
			result, operationErr = adapter.Execute(operation, integration.RequestFromInvocation(claim))
		}
		cancel()
		metrics.Operation(false, operationErr == nil, integration.IsUnknownOutcome(operationErr))
		if integration.IsUnknownOutcome(operationErr) {
			logger.WarnContext(ctx, "integration invocation outcome unknown", "error_class", "integration_outcome_unknown", "stage", integration.UnknownOutcomeStage(operationErr))
		}
		if err := completeInvocation(ctx, control, claim, result, operationErr); err != nil {
			return processed, err
		}
		processed++
	}
	sources, err := processConfigurationSourceWork(ctx, control.ConfigurationSources, adapter, metrics, config)
	processed += sources
	if err != nil {
		return processed, err
	}
	writebacks, err := processConfigurationWriteBackWork(ctx, control.ConfigurationWriteBacks, adapter, metrics, config)
	return processed + writebacks, err
}

// completeTest reports the adapter's classified outcome with the exact test lease, fence and generation, rejecting a missing lease locally.
// RU: completeTest передаёт классифицированный результат адаптера с точными lease, fence и generation проверки, локально отклоняя отсутствующую аренду.
// The idempotency key includes lease generation; owner validation and the RPC error remain authoritative, with no retry here.
// RU: Ключ идемпотентности включает поколение аренды; owner-проверка и RPC-ошибка остаются авторитетными, повтора здесь нет.
func completeTest(ctx context.Context, control *controlplaneclient.Client, claim *controlplanev1.IntegrationConnectionTestClaim, result string, operationErr error) error {
	lease := claim.GetLease()
	if lease == nil {
		return errors.New("integration test lease is missing")
	}
	success, code := integration.Outcome(operationErr)
	_, err := control.Runtime.CompleteIntegrationConnectionTest(ctx, &controlplanev1.CompleteIntegrationConnectionTestRequest{Mutation: &controlplanev1.MutationContext{IdempotencyKey: stableKey(claim.GetTestRef(), fmt.Sprintf("complete:%d", lease.GetGeneration()))}, TestRef: claim.GetTestRef(), LeaseRef: lease.GetRef(), Fence: lease.GetFence(), Generation: lease.GetGeneration(), Success: success, ResultSummary: result, SafeErrorCode: code})
	return err
}

// completeInvocation reports exact claim pins, the safe classified outcome and unknown-outcome flag to the owner without retrying completion.
// RU: completeInvocation передаёт владельцу точные привязки claim, безопасный классифицированный результат и признак неопределённости без повтора completion.
// Only success includes the adapter's effect receipt; failed or uncertain work never receives a fabricated successful receipt.
// RU: Только успех включает квитанцию эффекта адаптера; ошибка и неопределённость не получают выдуманную успешную квитанцию.
func completeInvocation(ctx context.Context, control *controlplaneclient.Client, claim *controlplanev1.IntegrationInvocationClaim, result integration.Result, operationErr error) error {
	lease := claim.GetLease()
	if lease == nil {
		return errors.New("integration invocation lease is missing")
	}
	success, code := integration.Outcome(operationErr)
	request := &controlplanev1.CompleteIntegrationInvocationRequest{
		Mutation:      &controlplanev1.MutationContext{IdempotencyKey: stableKey(claim.GetInvocationRef(), "complete")},
		InvocationRef: claim.GetInvocationRef(), LeaseRef: lease.GetRef(), Fence: lease.GetFence(),
		Generation: lease.GetGeneration(), Success: success, UnknownOutcome: integration.IsUnknownOutcome(operationErr), ResultSummary: result.Summary, SafeErrorCode: code,
	}
	if success {
		request.EffectReceipt = &controlplanev1.IntegrationEffectReceipt{
			EffectKey: result.Receipt.EffectKey, InputDigest: result.Receipt.InputDigest,
			ProviderEffectRef: result.Receipt.ProviderEffectRef, ResponseDigest: result.Receipt.ResponseDigest,
			ResultSummary: result.Summary,
		}
	}
	_, err := control.Runtime.CompleteIntegrationInvocation(ctx, request)
	return err
}

// stableKey derives a deterministic namespaced UUID from NUL-separated components for idempotency, not authorization.
// RU: stableKey выводит детерминированный UUID с пространством имён из разделённых NUL компонентов для идемпотентности, а не авторизации.
func stableKey(left, right string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(left+"\x00"+right)).String()
}
