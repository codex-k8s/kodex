package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

// sortedApprovalScopePaths returns a sorted copy of the supplied scope paths. It preserves
// duplicates and does not validate paths or mutate the input slice.
// sortedApprovalScopePaths возвращает отсортированную копию путей области одобрения. Сохраняет
// повторы, не проверяет пути и не меняет исходный срез.
func sortedApprovalScopePaths(paths []string) []string {
	result := append([]string{}, paths...)
	sort.Strings(result)
	return result
}

// approveIntegrationScope loads and locks an exact waiting invocation/grant/connection tied to the
// owner gate and root run, requiring a declared HUMAN_SCOPED capability and matching
// package/input/schema pins. It derives scope values from canonical bounded input, limits encoded
// values to 65536 bytes, inserts the bounded approval scope and binds exactly one invocation.
// Caller authorization and outer commit remain separate; payload paths do not create authority.
// approveIntegrationScope загружает и блокирует точные ожидающие invocation, grant и подключение,
// связанные с owner gate и корневым запуском, требуя объявленную HUMAN_SCOPED capability и совпадение
// пакета, входа и схемы. Выводит значения области из канонического ограниченного ввода,
// ограничивает JSON 65536 байтами, записывает ограниченную область одобрения и связывает ровно
// одну invocation. Авторизация вызывающего кода и внешний Commit отдельны; пути из payload не
// создают полномочий.
func (repository *Repository) approveIntegrationScope(
	ctx context.Context, tx pgx.Tx, current scope, gateID, invocationID, rootRunID, projectID string,
	requested *command.IntegrationApprovalSelection,
) error {
	var policy, capabilityKey, definitionVersion, definitionDigest, connectionRef, definitionKey string
	var organizationID, resolvedProjectID, resolvedRootRunID, agentID, connectionID, grantID string
	var inputDigest string
	var boundedInput []byte
	var grantVersion int64
	var paths []string
	var initiatorID, initiatorKind string
	var initiatorActive bool
	err := tx.QueryRow(ctx, queryCommandsResolvegateSelectScopedIntegrationInvocation,
		invocationID, current.organizationID, rootRunID, projectID, gateID,
	).Scan(&policy, &boundedInput, &inputDigest, &capabilityKey,
		&definitionVersion, &definitionDigest, &connectionRef, &definitionKey,
		&organizationID, &resolvedProjectID, &resolvedRootRunID,
		&agentID, &connectionID, &grantID, &grantVersion, &paths, &initiatorID, &initiatorKind, &initiatorActive)
	if err != nil || policy != "HUMAN_SCOPED" || agentID == "" ||
		organizationID != current.organizationID || resolvedProjectID != projectID || resolvedRootRunID != rootRunID {
		return errs.ErrConflict
	}
	definition, err := repository.integrationPackage(ctx, tx, current.organizationID,
		connectionRef, definitionKey, definitionVersion, definitionDigest)
	if err != nil {
		return err
	}
	capability, ok := definition.Capability(capabilityKey)
	if !ok || !capability.AllowsApprovalPolicy(policy) ||
		definition.Digest != definitionDigest {
		return errs.ErrConflict
	}
	var proposed *integrationpackage.ApprovalScopeConsent
	if requested != nil {
		proposed = &integrationpackage.ApprovalScopeConsent{DurationSeconds: requested.DurationSeconds, MaxEffects: requested.MaxEffects, Lifetime: requested.Lifetime}
	}
	consent, err := capability.ResolveApprovalScopeConsent(proposed)
	if err != nil {
		return errs.ErrInvalid
	}
	if consent.Lifetime == "INITIATOR_RESOURCE" && (initiatorID == "" || initiatorKind != "USER" || !initiatorActive) {
		return errs.ErrForbidden
	}
	approvalScope, err := capability.ResolveApprovalScope(paths, boundedInput)
	if err != nil {
		return errs.ErrConflict
	}
	canonicalInput, err := capability.ValidateInput(boundedInput)
	if err != nil {
		return errs.ErrConflict
	}
	actualDigest := sha256.Sum256(canonicalInput)
	if hex.EncodeToString(actualDigest[:]) != inputDigest {
		return errs.ErrConflict
	}
	schemaDigest, err := capability.InputSchemaDigest()
	if err != nil {
		return errs.ErrUnavailable
	}
	encodedValues, err := json.Marshal(approvalScope.Values)
	if err != nil || len(encodedValues) > 65536 {
		return errs.ErrConflict
	}
	var approvalScopeID string
	err = tx.QueryRow(ctx, queryCommandsResolvegateInsertIntegrationApprovalScope,
		organizationID, projectID, connectionID, grantID, rootRunID, agentID, gateID,
		capabilityKey, grantVersion, definitionDigest, schemaDigest,
		sortedApprovalScopePaths(paths), encodedValues, approvalScope.Digest,
		consent.MaxEffects, consent.DurationSeconds, consent.Lifetime, initiatorID,
	).Scan(&approvalScopeID)
	if err != nil {
		return serializableTransactionError(err, errs.ErrConflict)
	}
	tag, err := tx.Exec(ctx, queryCommandsResolvegateBindIntegrationApprovalScope,
		invocationID, approvalScopeID)
	if err != nil || tag.RowsAffected() != 1 {
		return serializableTransactionError(err, errs.ErrConflict)
	}
	return nil
}
