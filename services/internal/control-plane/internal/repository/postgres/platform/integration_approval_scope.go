package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

func sortedApprovalScopePaths(paths []string) []string {
	result := append([]string{}, paths...)
	sort.Strings(result)
	return result
}

// Одобрение владельца создаёт право на повторные эффекты из фактического
// invocation и закреплённого grant; модель не поставляет пути или authority.
func (repository *Repository) approveIntegrationScope(
	ctx context.Context, tx pgx.Tx, current scope, gateID, invocationID, rootRunID, projectID string,
) error {
	var policy, capabilityKey, definitionVersion, definitionDigest, connectionRef, definitionKey string
	var organizationID, resolvedProjectID, resolvedRootRunID, agentID, connectionID, grantID string
	var inputDigest string
	var boundedInput []byte
	var grantVersion int64
	var paths []string
	err := tx.QueryRow(ctx, queryCommandsResolvegateSelectScopedIntegrationInvocation,
		invocationID, current.organizationID, rootRunID, projectID,
	).Scan(&policy, &boundedInput, &inputDigest, &capabilityKey,
		&definitionVersion, &definitionDigest, &connectionRef, &definitionKey,
		&organizationID, &resolvedProjectID, &resolvedRootRunID,
		&agentID, &connectionID, &grantID, &grantVersion, &paths)
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
	if !ok || capability.ApprovalPolicy != policy || capability.Risk == "READ" ||
		definition.Digest != definitionDigest {
		return errs.ErrConflict
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
