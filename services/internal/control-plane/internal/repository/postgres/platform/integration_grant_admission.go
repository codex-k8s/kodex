package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/integration_grant_admission.sql
var queryIntegrationGrantAdmission string

//go:embed sql/integration_connection_authority.sql
var queryIntegrationConnectionAuthority string

// Та же owner-функция обслуживает selector и admission до receipt/OCC.
// Выдача не требует заранее существующего grant.
func (repository *Repository) authorizeIntegrationGrant(ctx context.Context, tx pgx.Tx, current scope, payload command.IntegrationGrantInput) error {
	if payload.ConnectionRef == "" || payload.CapabilityKey == "" || (payload.AgentRef == "") == (payload.WorkflowRef == "") {
		return errs.ErrInvalid
	}
	kind, ref := "AGENT", payload.AgentRef
	if payload.WorkflowRef != "" {
		kind, ref = "WORKFLOW", payload.WorkflowRef
	}
	var connectionVersion, recipientVersion int64
	var definitionKey, definitionVersion, definitionDigest, projectRef, reason string
	arguments := pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID,
		"authority_project_id": current.authorityProjectID, "connection_ref": payload.ConnectionRef,
		"recipient_kind": kind, "recipient_ref": ref, "capability_key": payload.CapabilityKey,
	}
	scan := func() error {
		return tx.QueryRow(ctx, queryIntegrationGrantAdmission, arguments).Scan(&connectionVersion, &definitionKey, &definitionVersion, &definitionDigest, &projectRef, &recipientVersion, &reason)
	}
	err := scan()
	if errors.Is(err, pgx.ErrNoRows) {
		// Неизвестный capability различим только после exact authority к обоим
		// владельцам. Скрытый target остаётся NotFound при любом payload key.
		arguments["capability_key"] = ""
		err = scan()
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if connectionVersion < 1 || recipientVersion < 1 || projectRef == "" {
		return errs.ErrUnavailable
	}
	if reason != "READY" && reason != "CONNECTION_UNAVAILABLE" && reason != "RECIPIENT_UNAVAILABLE" {
		return errs.ErrUnavailable
	}
	if payload.Enabled && reason != "READY" {
		return errs.ErrConflict
	}
	definition, err := repository.integrationPackage(ctx, tx, current.organizationID, payload.ConnectionRef, definitionKey, definitionVersion, definitionDigest)
	if err != nil {
		return err
	}
	capability, ok := definition.Capability(payload.CapabilityKey)
	if !ok {
		return errs.ErrInvalid
	}
	configuration, _, _, err := integrationCandidateScope(ctx, tx, current, integrationCandidateRow{ConnectionRef: payload.ConnectionRef, RecipientKind: kind, RecipientRef: ref, CapabilityKey: payload.CapabilityKey})
	if err != nil || definition.ValidateConfiguration(configuration) != nil {
		return errs.ErrUnavailable
	}
	if _, err := capability.ResourceScopeValues(configuration); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}
