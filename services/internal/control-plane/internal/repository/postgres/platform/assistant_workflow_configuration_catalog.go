package platform

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// assistantWorkflowConfigurationCatalogTx requires the exact UPDATE_WORKFLOW screen context and
// matching entity/version, reads the canonical project workflow snapshot and returns bounded full
// JSON/hash in the caller transaction. Missing/mismatched context is NotFound; no workflow
// mutation or publication occurs.
// assistantWorkflowConfigurationCatalogTx требует точный UPDATE_WORKFLOW screen context и
// совпадающие entity/version, читает канонический project workflow snapshot и возвращает
// ограниченный полный JSON/hash в транзакции caller. Отсутствующий/несовпавший context даёт
// NotFound; workflow mutation или publication не выполняется.
func (repository *Repository) assistantWorkflowConfigurationCatalogTx(ctx context.Context, tx pgx.Tx, current scope, leaseRef string, input entity.AssistantConfigurationCatalogRequest) (entity.AssistantConfigurationCatalogResponse, error) {
	result := entity.AssistantConfigurationCatalogResponse{Kind: input.Kind, AssistantRef: input.AssistantRef, ScopeKind: "PROJECT", OrganizationRef: current.organizationRef, Entries: []entity.AssistantConfigurationCatalogEntry{}}
	var contextKind, contextRef, name string
	var contextVersion, projectVersion int64
	err := tx.QueryRow(ctx, queryAssistantRecipientIntegrationCatalogContext, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "lease_ref": leaseRef, "required_operation": "UPDATE_WORKFLOW"}).Scan(&contextKind, &contextRef, &name, &contextVersion, &result.ProjectRef, &projectVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrNotFound
	}
	if err != nil {
		return result, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	if contextKind != "WORKFLOW" || input.EntityKind != contextKind || input.EntityRef != contextRef {
		return result, errs.ErrNotFound
	}
	before, version, err := repository.readAssistantWorkflowSnapshot(ctx, tx, current, result.ProjectRef, contextRef)
	if err != nil {
		return result, err
	}
	if version != contextVersion {
		return result, errs.ErrNotFound
	}
	raw, err := json.Marshal(before)
	if err != nil || len(raw) > 1<<20 {
		return result, errs.ErrUnavailable
	}
	result.WorkflowConfiguration = &entity.AssistantWorkflowConfiguration{WorkflowRef: contextRef, ProjectRef: result.ProjectRef, Version: version, ConfigurationJSON: raw, ConfigurationSHA256: assistantConfigurationSHA256(raw)}
	return result, nil
}
