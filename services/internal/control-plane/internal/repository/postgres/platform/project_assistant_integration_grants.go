package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

const changeProjectAssistantIntegrationGrant = "CHANGE_PROJECT_ASSISTANT_INTEGRATION_GRANT"

//go:embed sql/project_assistant_integration_grants__owner.sql
var queryProjectAssistantIntegrationGrantOwner string

type projectAssistantIntegrationGrantTarget struct {
	ref, name, projectID, projectRef, profileRef string
	version, profileVersion                      int64
	ready                                        bool
	connection                                   projectAssistantGrantConnectionHeader
}

type projectAssistantGrantConnectionHeader struct {
	ref, name, definitionKey, definitionVersion, definitionDigest string
	version                                                       int64
}

func (repository *Repository) projectAssistantIntegrationGrantOwner(ctx context.Context, tx pgx.Tx, current scope, assistantRef, connectionRef string) (projectAssistantIntegrationGrantTarget, error) {
	var target projectAssistantIntegrationGrantTarget
	err := tx.QueryRow(ctx, queryProjectAssistantIntegrationGrantOwner, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID,
		"assistant_ref": assistantRef, "connection_ref": connectionRef, "authority_project": current.authorityProjectID,
	}).Scan(&target.ref, &target.name, &target.version, &target.ready, &target.projectID, &target.projectRef, &target.profileRef, &target.profileVersion,
		&target.connection.ref, &target.connection.name, &target.connection.version,
		&target.connection.definitionKey, &target.connection.definitionVersion, &target.connection.definitionDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return target, errs.ErrNotFound
	}
	if err != nil {
		return target, errs.ErrUnavailable
	}
	if target.ref != assistantRef || target.projectRef == "" || target.profileRef == "" || target.version < 1 || target.profileVersion < 1 ||
		!validProjectAssistantGrantConnectionHeader(target.connection, connectionRef) {
		return target, errs.ErrUnavailable
	}
	return target, nil
}

func validProjectAssistantGrantConnectionHeader(header projectAssistantGrantConnectionHeader, connectionRef string) bool {
	return connectionRef != "" && header.ref == connectionRef && header.name != "" && header.version > 0 &&
		header.definitionKey != "" && header.definitionVersion != "" && validAssistantDefinitionDigest(header.definitionDigest)
}

func (repository *Repository) projectAssistantIntegrationGrantInput(ctx context.Context, tx pgx.Tx, current scope, raw any) (command.IntegrationGrantInput, projectAssistantIntegrationGrantTarget, error) {
	payload, ok := raw.(command.ProjectAssistantIntegrationGrantInput)
	if !ok || payload.AssistantRef == "" || payload.Grant.ConnectionRef == "" || payload.Grant.CapabilityKey == "" || !validIntegrationApprovalPolicy(payload.Grant.ApprovalPolicy) {
		return command.IntegrationGrantInput{}, projectAssistantIntegrationGrantTarget{}, errs.ErrInvalid
	}
	target, err := repository.projectAssistantIntegrationGrantOwner(ctx, tx, current, payload.AssistantRef, payload.Grant.ConnectionRef)
	if err != nil {
		return command.IntegrationGrantInput{}, target, err
	}
	canonical := command.IntegrationGrantInput{ConnectionRef: payload.Grant.ConnectionRef, CapabilityKey: payload.Grant.CapabilityKey,
		AgentRef: target.ref, Enabled: payload.Grant.Enabled, ApprovalPolicy: payload.Grant.ApprovalPolicy, ApprovalScopePaths: append([]string{}, payload.Grant.ApprovalScopePaths...)}
	// Тот же canonical admission, что у обычного адресного grant: package, readiness и policy.
	if err := repository.authorizeIntegrationGrant(ctx, tx, current, canonical); err != nil {
		return canonical, target, err
	}
	return canonical, target, nil
}

func projectAssistantGrantOwnerPins(target projectAssistantIntegrationGrantTarget, current scope) map[string]any {
	return map[string]any{"projectAssistantRef": target.ref, "assistantScope": "PROJECT", "scopeKind": "ORGANIZATION",
		"organizationRef": current.organizationRef, "projectRef": target.projectRef, "assistantProfileRef": target.profileRef,
		"agentVersion": target.version, "profileVersion": target.profileVersion}
}

func (repository *Repository) authorizeProjectAssistantIntegrationGrantPlan(ctx context.Context, tx pgx.Tx, current scope, operations []entity.AssistantPlanOperation) error {
	for _, operation := range operations {
		if operation.Type != changeProjectAssistantIntegrationGrant {
			continue
		}
		target, err := repository.projectAssistantIntegrationGrantOwner(ctx, tx, current, assistantString(operation.Parameters, "projectAssistantRef"), assistantString(operation.Parameters, "connectionRef"))
		if err != nil {
			return err
		}
		pins := projectAssistantGrantOwnerPins(target, current)
		for _, field := range []string{"projectAssistantRef", "assistantScope", "scopeKind", "organizationRef", "projectRef", "assistantProfileRef"} {
			if !assistantJSONEqual(operation.Parameters[field], pins[field]) {
				return errs.ErrNotFound
			}
		}
	}
	return nil
}
