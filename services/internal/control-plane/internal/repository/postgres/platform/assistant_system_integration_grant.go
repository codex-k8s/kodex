package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/system_assistant_integration_grants__snapshot.sql
var querySystemAssistantIntegrationGrantSnapshot string

//go:embed sql/system_assistant_integration_grants__plan_authority.sql
var querySystemAssistantIntegrationGrantPlanAuthority string

func (repository *Repository) authorizeSystemAssistantIntegrationGrantPlan(ctx context.Context, tx pgx.Tx, current scope, input command.Command) error {
	var ref string
	switch payload := input.Payload.(type) {
	case command.AssistantPlanInput:
		ref = payload.PlanRef
	case command.AssistantPlanDraftInput:
		ref = payload.PlanRef
	default:
		return nil
	}
	var raw []byte
	err := tx.QueryRow(ctx, querySystemAssistantIntegrationGrantPlanAuthority, current.organizationID, current.actorID, ref).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	var operations []entity.AssistantPlanOperation
	if json.Unmarshal(raw, &operations) != nil || len(operations) > maximumAssistantPlanOperations {
		return errs.ErrUnavailable
	}
	if err := repository.authorizeProjectAssistantIntegrationGrantPlan(ctx, tx, current, operations); err != nil {
		return err
	}
	for _, operation := range operations {
		if operation.Type != "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT" {
			continue
		}
		if assistantString(operation.Parameters, "scopeKind") != "ORGANIZATION" || assistantString(operation.Parameters, "organizationRef") != current.organizationRef {
			return errs.ErrNotFound
		}
		owner, err := repository.systemAssistantIntegrationGrantOwner(ctx, tx, current, assistantString(operation.Parameters, "connectionRef"))
		if err != nil {
			return err
		}
		if owner.ref != assistantString(operation.Parameters, "systemAssistantRef") {
			return errs.ErrNotFound
		}
	}
	return nil
}

var assistantSystemGrantEditable = []string{"connectionRef", "capabilityKey", "enabled", "approvalPolicy", "approvalScopePaths"}
var assistantSystemGrantOwner = []string{"scopeKind", "organizationRef", "systemAssistantRef", "definitionVersion", "definitionDigest", "grantRef", "grantVersion", "defaultApprovalPolicy", "allowedApprovalPolicies"}

func (repository *Repository) hydrateSystemAssistantIntegrationGrant(ctx context.Context, tx pgx.Tx, current scope, operation entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	if !onlyAssistantFields(operation.Parameters, assistantSystemGrantEditable...) {
		return operation, errs.ErrInvalid
	}
	enabled, enabledOK := assistantBoolValue(operation.Parameters, "enabled")
	paths := []string{}
	if _, supplied := operation.Parameters["approvalScopePaths"]; supplied {
		var ok bool
		paths, ok = assistantStringsValue(operation.Parameters, "approvalScopePaths")
		if !ok {
			return operation, errs.ErrInvalid
		}
	}
	payload := command.SystemAssistantIntegrationGrantInput{ConnectionRef: assistantString(operation.Parameters, "connectionRef"), CapabilityKey: assistantString(operation.Parameters, "capabilityKey"), ApprovalPolicy: assistantString(operation.Parameters, "approvalPolicy"), Enabled: enabled, ApprovalScopePaths: paths}
	if !enabledOK || !validCapabilityKey(payload.CapabilityKey) || len(paths) > 16 || !enabled && len(paths) != 0 {
		return operation, errs.ErrInvalid
	}
	_, target, err := repository.systemAssistantIntegrationGrantInput(ctx, tx, current, payload)
	if err != nil {
		return operation, err
	}
	connection, err := readConnection(ctx, tx, current, payload.ConnectionRef)
	if err != nil {
		return operation, err
	}
	definition, err := repository.integrationPackage(ctx, tx, current.organizationID, connection.Ref, connection.DefinitionKey, connection.DefinitionVersion, connection.DefinitionDigest)
	if err != nil {
		return operation, err
	}
	capability, ok := definition.Capability(payload.CapabilityKey)
	if !ok || !capability.CallableByAgent() || !capability.AllowsApprovalPolicy(payload.ApprovalPolicy) {
		return operation, errs.ErrInvalid
	}
	if payload.ApprovalPolicy == "HUMAN_SCOPED" && enabled {
		if capability.ValidateApprovalScopePaths(paths) != nil {
			return operation, errs.ErrInvalid
		}
	} else if len(paths) != 0 {
		return operation, errs.ErrInvalid
	}
	var grantRef, policy string
	var grantVersion int64
	var existingEnabled bool
	var existingPaths []string
	if err = tx.QueryRow(ctx, querySystemAssistantIntegrationGrantSnapshot, current.organizationID, connection.Ref, target.ref, payload.CapabilityKey).Scan(&grantRef, &grantVersion, &policy, &existingEnabled, &existingPaths); err != nil {
		return operation, errs.ErrUnavailable
	}
	if grantRef == "" {
		policy = capability.ApprovalPolicy
	}
	if enabled == existingEnabled && policy == payload.ApprovalPolicy && assistantJSONEqual(sortedApprovalScopePaths(existingPaths), sortedApprovalScopePaths(paths)) {
		return operation, errs.ErrConflict
	}
	params := map[string]any{"connectionRef": connection.Ref, "capabilityKey": payload.CapabilityKey, "enabled": enabled, "approvalPolicy": payload.ApprovalPolicy, "approvalScopePaths": append([]string{}, paths...), "scopeKind": "ORGANIZATION", "organizationRef": current.organizationRef, "systemAssistantRef": target.ref, "definitionVersion": connection.DefinitionVersion, "definitionDigest": connection.DefinitionDigest, "grantRef": grantRef, "grantVersion": grantVersion, "defaultApprovalPolicy": capability.ApprovalPolicy, "allowedApprovalPolicies": append([]string{}, capability.AllowedApprovalPolicies...)}
	before := cloneAssistantFields(params)
	before["enabled"], before["approvalPolicy"], before["approvalScopePaths"] = existingEnabled, policy, append([]string{}, existingPaths...)
	operation.Parameters, operation.Before, operation.After = params, before, cloneAssistantFields(params)
	version := connection.Version
	operation.ExpectedVersion = &version
	operation.Target = entity.AssistantPlanTarget{Kind: "INTEGRATION_CONNECTION", Ref: connection.Ref, Name: connection.Name, Version: &version}
	operation.Action = "UPDATE"
	operation.Selected = true
	return operation, nil
}

func systemAssistantIntegrationGrantCommand(operation entity.AssistantPlanOperation) (command.Command, error) {
	allowed := append(append([]string{}, assistantSystemGrantEditable...), assistantSystemGrantOwner...)
	allowed = append(allowed, "expectedVersion")
	if !onlyAssistantFields(operation.Input, allowed...) || !hasAssistantFields(operation.Input, append(append([]string{}, assistantSystemGrantEditable...), assistantSystemGrantOwner...)...) {
		return command.Command{}, errs.ErrInvalid
	}
	version, versionOK := assistantInt64(operation.Input, "expectedVersion")
	enabled, enabledOK := assistantBoolValue(operation.Input, "enabled")
	paths, pathsOK := assistantStringsValue(operation.Input, "approvalScopePaths")
	if !versionOK || version < 1 || !enabledOK || !pathsOK || !validIntegrationApprovalPolicy(assistantString(operation.Input, "approvalPolicy")) || assistantString(operation.Input, "scopeKind") != "ORGANIZATION" || assistantString(operation.Input, "organizationRef") == "" || assistantString(operation.Input, "systemAssistantRef") == "" {
		return command.Command{}, errs.ErrInvalid
	}
	return command.Command{Kind: command.ChangeSystemAssistantIntegrationGrant, Mutation: value.Mutation{ExpectedVersion: &version}, Payload: command.SystemAssistantIntegrationGrantInput{ConnectionRef: assistantString(operation.Input, "connectionRef"), CapabilityKey: assistantString(operation.Input, "capabilityKey"), ApprovalPolicy: assistantString(operation.Input, "approvalPolicy"), Enabled: enabled, ApprovalScopePaths: paths}}, nil
}

func (repository *Repository) systemAssistantIntegrationGrantSnapshotMatches(ctx context.Context, tx pgx.Tx, current scope, original entity.AssistantPlanOperation) (bool, error) {
	request := original
	request.Parameters = map[string]any{}
	for _, field := range assistantSystemGrantEditable {
		request.Parameters[field] = original.Parameters[field]
	}
	fresh, err := repository.hydrateSystemAssistantIntegrationGrant(ctx, tx, current, request)
	if err != nil {
		return false, err
	}
	return assistantJSONEqual(fresh.Before, original.Before) && assistantJSONEqual(fresh.After, original.After) && assistantJSONEqual(fresh.ExpectedVersion, original.ExpectedVersion) && assistantJSONEqual(fresh.Target, original.Target), nil
}

func (repository *Repository) rehydrateEditedSystemAssistantGrant(ctx context.Context, tx pgx.Tx, current scope, original, edited entity.AssistantPlanOperation, stale bool) (entity.AssistantPlanOperation, error) {
	if !onlyAssistantFields(edited.Parameters, append(append([]string{}, assistantSystemGrantEditable...), assistantSystemGrantOwner...)...) {
		return edited, errs.ErrInvalid
	}
	for _, field := range assistantSystemGrantOwner {
		if !assistantJSONEqual(original.Parameters[field], edited.Parameters[field]) {
			return edited, errs.ErrForbidden
		}
	}
	for _, field := range []string{"connectionRef", "capabilityKey"} {
		if !assistantJSONEqual(original.Parameters[field], edited.Parameters[field]) {
			return edited, errs.ErrForbidden
		}
	}
	if !stale {
		same, err := repository.systemAssistantIntegrationGrantSnapshotMatches(ctx, tx, current, original)
		if err != nil || !same {
			return edited, errs.ErrConflict
		}
	}
	request := edited
	request.Parameters = map[string]any{}
	for _, field := range assistantSystemGrantEditable {
		request.Parameters[field] = edited.Parameters[field]
	}
	fresh, err := repository.hydrateSystemAssistantIntegrationGrant(ctx, tx, current, request)
	if err != nil {
		return edited, err
	}
	for _, field := range []string{"scopeKind", "organizationRef", "systemAssistantRef"} {
		if !assistantJSONEqual(fresh.Parameters[field], original.Parameters[field]) {
			return edited, errs.ErrForbidden
		}
	}
	fresh.Selected = edited.Selected
	return fresh, nil
}
