package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

var assistantProjectGrantEditable = []string{"projectAssistantRef", "connectionRef", "capabilityKey", "enabled", "approvalPolicy", "approvalScopePaths"}
var assistantProjectGrantOwner = []string{"assistantScope", "scopeKind", "organizationRef", "projectRef", "assistantProfileRef", "agentVersion", "profileVersion", "definitionVersion", "definitionDigest", "grantRef", "grantVersion", "defaultApprovalPolicy", "allowedApprovalPolicies"}

func (repository *Repository) hydrateProjectAssistantIntegrationGrant(ctx context.Context, tx pgx.Tx, current scope, operation entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	if !onlyAssistantFields(operation.Parameters, assistantProjectGrantEditable...) {
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
	_, target, err := repository.projectAssistantIntegrationGrantInput(ctx, tx, current, command.ProjectAssistantIntegrationGrantInput{AssistantRef: assistantString(operation.Parameters, "projectAssistantRef"), Grant: payload})
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
	params := map[string]any{"connectionRef": connection.Ref, "capabilityKey": payload.CapabilityKey, "enabled": enabled, "approvalPolicy": payload.ApprovalPolicy, "approvalScopePaths": append([]string{}, paths...), "scopeKind": "ORGANIZATION", "organizationRef": current.organizationRef, "projectAssistantRef": target.ref, "definitionVersion": connection.DefinitionVersion, "definitionDigest": connection.DefinitionDigest, "grantRef": grantRef, "grantVersion": grantVersion, "defaultApprovalPolicy": capability.ApprovalPolicy, "allowedApprovalPolicies": append([]string{}, capability.AllowedApprovalPolicies...)}
	for field, value := range projectAssistantGrantOwnerPins(target, current) {
		params[field] = value
	}
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

func projectAssistantIntegrationGrantCommand(operation entity.AssistantPlanOperation) (command.Command, error) {
	allowed := append(append([]string{}, assistantProjectGrantEditable...), assistantProjectGrantOwner...)
	allowed = append(allowed, "expectedVersion")
	if !onlyAssistantFields(operation.Input, allowed...) || !hasAssistantFields(operation.Input, append(append([]string{}, assistantProjectGrantEditable...), assistantProjectGrantOwner...)...) {
		return command.Command{}, errs.ErrInvalid
	}
	version, versionOK := assistantInt64(operation.Input, "expectedVersion")
	agentVersion, agentOK := assistantInt64(operation.Input, "agentVersion")
	profileVersion, profileOK := assistantInt64(operation.Input, "profileVersion")
	enabled, enabledOK := assistantBoolValue(operation.Input, "enabled")
	paths, pathsOK := assistantStringsValue(operation.Input, "approvalScopePaths")
	if !versionOK || version < 1 || !agentOK || agentVersion < 1 || !profileOK || profileVersion < 1 || assistantString(operation.Input, "assistantScope") != "PROJECT" || assistantString(operation.Input, "projectRef") == "" || assistantString(operation.Input, "assistantProfileRef") == "" || !enabledOK || !pathsOK || !validIntegrationApprovalPolicy(assistantString(operation.Input, "approvalPolicy")) || assistantString(operation.Input, "scopeKind") != "ORGANIZATION" || assistantString(operation.Input, "organizationRef") == "" || assistantString(operation.Input, "projectAssistantRef") == "" {
		return command.Command{}, errs.ErrInvalid
	}
	return command.Command{Kind: command.ChangeProjectAssistantIntegrationGrant, Mutation: value.Mutation{ExpectedVersion: &version}, Payload: command.ProjectAssistantIntegrationGrantInput{AssistantRef: assistantString(operation.Input, "projectAssistantRef"), Grant: command.SystemAssistantIntegrationGrantInput{ConnectionRef: assistantString(operation.Input, "connectionRef"), CapabilityKey: assistantString(operation.Input, "capabilityKey"), ApprovalPolicy: assistantString(operation.Input, "approvalPolicy"), Enabled: enabled, ApprovalScopePaths: paths}}}, nil
}

func (repository *Repository) projectAssistantIntegrationGrantSnapshotMatches(ctx context.Context, tx pgx.Tx, current scope, original entity.AssistantPlanOperation) (bool, error) {
	request := original
	request.Parameters = map[string]any{}
	for _, field := range assistantProjectGrantEditable {
		request.Parameters[field] = original.Parameters[field]
	}
	fresh, err := repository.hydrateProjectAssistantIntegrationGrant(ctx, tx, current, request)
	if err != nil {
		return false, err
	}
	return assistantJSONEqual(fresh.Before, original.Before) && assistantJSONEqual(fresh.After, original.After) && assistantJSONEqual(fresh.ExpectedVersion, original.ExpectedVersion) && assistantJSONEqual(fresh.Target, original.Target), nil
}

func (repository *Repository) rehydrateEditedProjectAssistantGrant(ctx context.Context, tx pgx.Tx, current scope, original, edited entity.AssistantPlanOperation, stale bool) (entity.AssistantPlanOperation, error) {
	if !onlyAssistantFields(edited.Parameters, append(append([]string{}, assistantProjectGrantEditable...), assistantProjectGrantOwner...)...) {
		return edited, errs.ErrInvalid
	}
	for _, field := range assistantProjectGrantOwner {
		if !assistantJSONEqual(original.Parameters[field], edited.Parameters[field]) {
			return edited, errs.ErrForbidden
		}
	}
	for _, field := range []string{"projectAssistantRef", "connectionRef", "capabilityKey"} {
		if !assistantJSONEqual(original.Parameters[field], edited.Parameters[field]) {
			return edited, errs.ErrForbidden
		}
	}
	if !stale {
		same, err := repository.projectAssistantIntegrationGrantSnapshotMatches(ctx, tx, current, original)
		if err != nil || !same {
			return edited, errs.ErrConflict
		}
	}
	request := edited
	request.Parameters = map[string]any{}
	for _, field := range assistantProjectGrantEditable {
		request.Parameters[field] = edited.Parameters[field]
	}
	fresh, err := repository.hydrateProjectAssistantIntegrationGrant(ctx, tx, current, request)
	if err != nil {
		return edited, err
	}
	for _, field := range []string{"assistantScope", "scopeKind", "organizationRef", "projectRef", "assistantProfileRef", "projectAssistantRef"} {
		if !assistantJSONEqual(fresh.Parameters[field], original.Parameters[field]) {
			return edited, errs.ErrForbidden
		}
	}
	fresh.Selected = edited.Selected
	return fresh, nil
}
