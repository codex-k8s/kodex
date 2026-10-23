package app

import (
	"context"
	"log/slog"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/external/interaction-gateway/internal/mattermost"
)

// invocationExecutor separates exact integration execution and connection probing from owner-managed work claims and receipts.
// RU: invocationExecutor отделяет точное исполнение интеграции и проверку подключения от управляемых владельцем claim и квитанций.
type invocationExecutor interface {
	// Execute performs one claimed integration operation and returns its effect receipt without completing the owner's invocation.
	// RU: Execute выполняет одну арендованную операцию интеграции и возвращает квитанцию эффекта без завершения invocation у владельца.
	Execute(context.Context, *controlplanev1.IntegrationInvocationClaim) (*controlplanev1.IntegrationEffectReceipt, error)
	// TestConnection probes the claimed connection and returns a safe summary or an error for owner-side completion.
	// RU: TestConnection проверяет арендованное подключение и возвращает безопасное резюме либо ошибку для завершения у владельца.
	TestConnection(context.Context, *controlplanev1.IntegrationConnectionTestClaim) (string, error)
}

// runInvocationLoop bounds each polling cycle, reports degradation transitions without raw dependency errors and stops on cancellation.
// RU: runInvocationLoop ограничивает каждый цикл опроса, сообщает переходы деградации без сырых ошибок зависимостей и останавливается по отмене.
func runInvocationLoop(control controlplanev1.RuntimeWorkServiceClient, adapter invocationExecutor, logger *slog.Logger, config Config) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(config.PollInterval)
		defer ticker.Stop()
		degraded := false
		for {
			cycle, cancel := context.WithTimeout(ctx, config.OperationTimeout)
			err := processIntegrationWork(cycle, control, adapter, config)
			cancel()
			if err != nil && !degraded {
				degraded = true
				logger.WarnContext(ctx, "interaction invocation degraded", "error_class", "control_plane_or_adapter")
			} else if err == nil && degraded {
				degraded = false
				logger.InfoContext(ctx, "interaction invocation restored")
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}
}

// processIntegrationWork claims at most one connection test and one invocation per iteration, completing each before another claim of its kind.
// RU: processIntegrationWork получает не более одной проверки подключения и одного вызова за итерацию, завершая каждый до следующего claim того же вида.
// An idle response ends polling; malformed responses, cancellation or insufficient operation budget prevent further claims.
// RU: Пустой ответ завершает опрос; неверные ответы, отмена или недостаточный бюджет операции предотвращают следующие claim.
func processIntegrationWork(ctx context.Context, control controlplanev1.RuntimeWorkServiceClient, adapter invocationExecutor, config Config) error {
	for index := int32(0); index < config.ClaimLimit; index++ {
		if available, err := claimBudgetAvailable(ctx, config.RequestTimeout); err != nil || !available {
			return err
		}
		query, cancel := context.WithTimeout(ctx, config.RequestTimeout)
		tests, err := control.ClaimIntegrationConnectionTests(query, &controlplanev1.ClaimIntegrationConnectionTestsRequest{WorkloadInstance: config.InstanceID, Limit: 1})
		cancel()
		if err != nil {
			return err
		}
		if tests == nil || len(tests.GetClaims()) > 1 {
			return errDeliveryResponse
		}
		if len(tests.GetClaims()) == 1 {
			if err := processConnectionTest(ctx, control, adapter, tests.GetClaims()[0], config); err != nil {
				return err
			}
		}
		query, cancel = context.WithTimeout(ctx, config.RequestTimeout)
		invocations, err := control.ClaimIntegrationInvocations(query, &controlplanev1.ClaimIntegrationInvocationsRequest{WorkloadInstance: config.InstanceID, Limit: 1})
		cancel()
		if err != nil {
			return err
		}
		if invocations == nil || len(invocations.GetClaims()) > 1 {
			return errDeliveryResponse
		}
		if len(invocations.GetClaims()) == 0 {
			if len(tests.GetClaims()) == 0 {
				return nil
			}
			continue
		}
		if err := processInvocation(ctx, control, adapter, invocations.GetClaims()[0], config); err != nil {
			return err
		}
	}
	return nil
}

// processConnectionTest validates the Mattermost claim and lease, probes within its deadline and reports exact lease pins to the owner.
// RU: processConnectionTest проверяет Mattermost claim и аренду, выполняет проверку в её срок и передаёт точные координаты аренды владельцу.
// Completion must read back the same connection; a probe error is preserved after successful completion reporting.
// RU: Ответ завершения должен ссылаться на то же подключение; ошибка проверки сохраняется после успешной передачи результата.
func processConnectionTest(ctx context.Context, control controlplanev1.RuntimeWorkServiceClient, adapter invocationExecutor, claim *controlplanev1.IntegrationConnectionTestClaim, config Config) error {
	if claim.GetTestRef() == "" || claim.GetConnectionRef() == "" || claim.GetDefinitionKey() != "mattermost" {
		return errDeliveryResponse
	}
	operation, cancel, err := leaseContext(ctx, claim.GetLease(), config.RequestTimeout)
	if err != nil {
		return err
	}
	summary, operationErr := adapter.TestConnection(operation, claim)
	cancel()
	success, code, _ := mattermost.InvocationOutcome(operationErr)
	lease := claim.GetLease()
	completion, cancelCompletion := context.WithTimeout(ctx, config.RequestTimeout)
	defer cancelCompletion()
	response, err := control.CompleteIntegrationConnectionTest(completion, &controlplanev1.CompleteIntegrationConnectionTestRequest{
		Mutation: &controlplanev1.MutationContext{IdempotencyKey: workCompletionKey(claim.GetTestRef(), lease)},
		TestRef:  claim.GetTestRef(), LeaseRef: lease.GetRef(), Fence: lease.GetFence(), Generation: lease.GetGeneration(),
		Success: success, SafeErrorCode: code, ResultSummary: summary,
	})
	if err != nil {
		return err
	}
	if response.GetConnection().GetRef() != claim.GetConnectionRef() {
		return errDeliveryResponse
	}
	return operationErr
}

// processInvocation validates and executes one leased Mattermost operation, checking receipt identity and required result fields before reporting success.
// RU: processInvocation проверяет и выполняет одну арендованную Mattermost-операцию, сверяя идентичность квитанции и обязательные поля результата до передачи успеха.
// Invalid write receipts become unknown outcomes; completion uses a separate bounded context and never attaches a rejected receipt.
// RU: Неверные квитанции записи становятся неизвестным исходом; завершение использует отдельный ограниченный контекст и не прикладывает отклонённую квитанцию.
func processInvocation(ctx context.Context, control controlplanev1.RuntimeWorkServiceClient, adapter invocationExecutor, claim *controlplanev1.IntegrationInvocationClaim, config Config) error {
	if claim.GetInvocationRef() == "" || claim.GetDefinitionKey() != "mattermost" {
		return errDeliveryResponse
	}
	operation, cancel, err := leaseContext(ctx, claim.GetLease(), config.RequestTimeout)
	if err != nil {
		return err
	}
	receipt, operationErr := adapter.Execute(operation, claim)
	cancel()
	success, code, unknown := mattermost.InvocationOutcome(operationErr)
	if success && (receipt == nil || receipt.GetEffectKey() != claim.GetEffectKey() || receipt.GetInputDigest() != claim.GetInputDigest() || receipt.GetProviderEffectRef() == "" || len(receipt.GetResponseDigest()) != 64 || receipt.GetResultSummary() == "") {
		code, success = "INTEGRATION_RESPONSE_INVALID", false
		if claim.GetRisk() != controlplanev1.IntegrationRisk_INTEGRATION_RISK_READ {
			code, unknown = "INTEGRATION_OUTCOME_UNKNOWN", true
		}
	}
	lease := claim.GetLease()
	request := &controlplanev1.CompleteIntegrationInvocationRequest{
		Mutation:      &controlplanev1.MutationContext{IdempotencyKey: workCompletionKey(claim.GetInvocationRef(), lease)},
		InvocationRef: claim.GetInvocationRef(), LeaseRef: lease.GetRef(), Fence: lease.GetFence(), Generation: lease.GetGeneration(),
		Success: success, SafeErrorCode: code, UnknownOutcome: unknown,
	}
	if success {
		request.EffectReceipt, request.ResultSummary = receipt, receipt.GetResultSummary()
	}
	completion, cancelCompletion := context.WithTimeout(ctx, config.RequestTimeout)
	defer cancelCompletion()
	response, err := control.CompleteIntegrationInvocation(completion, request)
	if err != nil {
		return err
	}
	if response.GetRun().GetRef() == "" {
		return errDeliveryResponse
	}
	if !success && operationErr == nil {
		return errDeliveryResponse
	}
	return operationErr
}
