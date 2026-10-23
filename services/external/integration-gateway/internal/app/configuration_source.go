package app

import (
	"context"
	"errors"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
	businessmetrics "github.com/codex-k8s/kodex/services/external/integration-gateway/internal/observability/metrics"
	"google.golang.org/protobuf/proto"
)

// maximumConfigurationSourceOperation caps provider source reads independently of a larger deployment operation timeout.
// RU: maximumConfigurationSourceOperation ограничивает чтение источника у провайдера независимо от большего runtime-таймаута операции.
const maximumConfigurationSourceOperation = 20 * time.Second

// configurationSourceAdapter separates provider reads from owner claim and completion RPCs, which remain in the application loop.
// RU: configurationSourceAdapter отделяет чтение у провайдера от owner RPC получения и завершения работы, остающихся в прикладном цикле.
type configurationSourceAdapter interface {
	// ReadConfigurationSource reads the claimed revision within the supplied deadline and returns content with provenance for owner validation.
	// RU: ReadConfigurationSource читает назначенную ревизию в пределах переданного срока и возвращает содержимое с происхождением для owner-проверки.
	ReadConfigurationSource(context.Context, *cp.ManagedConfigurationSourceWork) (integration.ConfigurationSourceResult, error)
}

// processConfigurationSourceWork claims at most one read and reserves one owner request before both lease expiry and work deadline.
// RU: processConfigurationSourceWork получает не более одного чтения и резервирует один owner-запрос до конца аренды и срока работы.
// It clears returned bytes and reports classified failure or exact completion once, rejecting foreign or inconsistent receipts.
// RU: Функция очищает возвращённые байты и однократно сообщает классифицированную ошибку либо точное завершение, отклоняя чужие или несогласованные квитанции.
// A processed SYNC_BLOCKED receipt is not an accepted configuration; READY additionally requires matching commit and content hashes.
// RU: Обработанная квитанция SYNC_BLOCKED не означает принятую конфигурацию; READY дополнительно требует совпадения commit и хеша содержимого.
// Parent cancellation prevents finalization, and a lost completion acknowledgement is returned without replaying the provider read.
// RU: Отмена родителя запрещает финализацию, а потерянное подтверждение завершения возвращается без повтора чтения у провайдера.
func processConfigurationSourceWork(ctx context.Context, control cp.ManagedConfigurationSourceWorkServiceClient, adapter configurationSourceAdapter, metrics *businessmetrics.Metrics, config Config) (int, error) {
	if control == nil {
		return 0, errors.New("configuration source owner is unavailable")
	}
	claimContext, cancelClaim := context.WithTimeout(ctx, config.RequestTimeout)
	claimed, err := control.ClaimManagedConfigurationSourceWork(claimContext, &cp.ClaimManagedConfigurationSourceWorkRequest{Claimant: config.InstanceID, Limit: 1})
	cancelClaim()
	if err != nil {
		return 0, err
	}
	if claimed == nil || len(claimed.ProtoReflect().GetUnknown()) != 0 || len(claimed.GetWork()) > 1 {
		return 0, errors.New("configuration source claim response is invalid")
	}
	if len(claimed.GetWork()) == 0 {
		return 0, nil
	}
	work := claimed.GetWork()[0]
	lease := work.GetLease()
	if work == nil || lease == nil || lease.GetClaimant() != config.InstanceID || lease.GetExpiresAt() == nil || lease.GetExpiresAt().CheckValid() != nil || work.GetDeadline() == nil || work.GetDeadline().CheckValid() != nil {
		return 0, errors.New("configuration source lease is invalid")
	}
	budget := min(config.OperationTimeout, maximumConfigurationSourceOperation)
	deadline := time.Now().Add(budget)
	finalizeBefore := lease.GetExpiresAt().AsTime().Add(-config.RequestTimeout)
	if work.GetDeadline().AsTime().Add(-config.RequestTimeout).Before(finalizeBefore) {
		finalizeBefore = work.GetDeadline().AsTime().Add(-config.RequestTimeout)
	}
	if finalizeBefore.Before(deadline) {
		deadline = finalizeBefore
	}
	if !deadline.After(time.Now()) {
		return 0, errors.New("configuration source lease budget is insufficient")
	}
	operation, cancelOperation := context.WithDeadline(ctx, deadline)
	result, operationErr := adapter.ReadConfigurationSource(operation, work)
	if operation.Err() != nil {
		operationErr = operation.Err()
	}
	cancelOperation()
	defer clear(result.Content)
	metrics.ConfigurationSource(operationErr == nil)
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	finalize, cancelFinalize := context.WithTimeout(ctx, config.RequestTimeout)
	defer cancelFinalize()
	if operationErr != nil {
		response, err := control.FailManagedConfigurationSourceWork(finalize, &cp.FailManagedConfigurationSourceWorkRequest{Lease: proto.Clone(lease).(*cp.ManagedConfigurationSourceLease), FailureCode: integration.ConfigurationSourceFailure(operationErr)})
		if err != nil {
			return 0, err
		}
		if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || !validConfigurationSourceReceipt(response.GetSource(), work) ||
			(response.GetSource().GetState() != cp.ManagedConfigurationSourceState_MANAGED_CONFIGURATION_SOURCE_STATE_QUEUED && response.GetSource().GetState() != cp.ManagedConfigurationSourceState_MANAGED_CONFIGURATION_SOURCE_STATE_SYNC_BLOCKED) {
			return 0, errors.New("configuration source failure receipt is missing")
		}
		return 1, nil
	}
	response, err := control.CompleteManagedConfigurationSourceWork(finalize, &cp.CompleteManagedConfigurationSourceWorkRequest{Lease: proto.Clone(lease).(*cp.ManagedConfigurationSourceLease), CommitSha: result.CommitSHA, ContentSha256: result.ContentSHA256, Content: result.Content, Ancestry: result.Ancestry})
	if err != nil {
		return 0, err
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || !validConfigurationSourceReceipt(response.GetSource(), work) ||
		(response.GetSource().GetState() != cp.ManagedConfigurationSourceState_MANAGED_CONFIGURATION_SOURCE_STATE_READY && response.GetSource().GetState() != cp.ManagedConfigurationSourceState_MANAGED_CONFIGURATION_SOURCE_STATE_SYNC_BLOCKED) {
		return 0, errors.New("configuration source completion receipt is missing")
	}
	if response.GetSource().GetState() == cp.ManagedConfigurationSourceState_MANAGED_CONFIGURATION_SOURCE_STATE_READY && (response.GetSource().GetAcceptedCommitSha() != result.CommitSHA || response.GetSource().GetAcceptedContentSha256() != result.ContentSHA256) {
		return 0, errors.New("configuration source accepted content does not match")
	}
	return 1, nil
}

// validConfigurationSourceReceipt requires a known-field versioned source with the exact claimed generation, connection and repository path.
// RU: validConfigurationSourceReceipt требует источник без неизвестных полей, с версией и точными назначенными поколением, подключением и путём репозитория.
// State and accepted-content checks belong to completion or failure branches, not this common identity predicate.
// RU: Проверки состояния и принятого содержимого принадлежат веткам завершения либо ошибки, а не этому общему предикату идентичности.
func validConfigurationSourceReceipt(source *cp.ManagedConfigurationGitSource, work *cp.ManagedConfigurationSourceWork) bool {
	return source != nil && len(source.ProtoReflect().GetUnknown()) == 0 && source.GetRef() == work.GetSourceRef() && source.GetVersion() > 0 && source.GetGeneration() == work.GetLease().GetSourceGeneration() && source.GetConnectionRef() == work.GetConnectionRef() && source.GetProviderKey() == work.GetDefinitionKey() && source.GetRepositoryRef() == work.GetRepositoryRef() && source.GetRefName() == work.GetRefName() && source.GetPath() == work.GetPath()
}
