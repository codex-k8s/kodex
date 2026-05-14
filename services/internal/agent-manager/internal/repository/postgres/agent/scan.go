package agent

import (
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

func scanFlow(row postgreslib.RowScanner) (entity.Flow, error) {
	var flow entity.Flow
	var displayName, description []byte
	var status string
	var activeVersionID pgtype.UUID
	err := row.Scan(
		&flow.ID,
		&flow.Scope.Type,
		&flow.Scope.Ref,
		&flow.Slug,
		&displayName,
		&description,
		&flow.IconObjectURI,
		&status,
		&activeVersionID,
		&flow.Version,
		&flow.CreatedAt,
		&flow.UpdatedAt,
	)
	flow.Status = enum.FlowStatus(status)
	flow.ActiveVersionID = postgreslib.UUIDPtrFromPG(activeVersionID)
	if err != nil {
		return flow, err
	}
	flow.DisplayName, err = localizedTextFromPayload(displayName)
	if err != nil {
		return flow, fmt.Errorf("scan flow display_name: %w", err)
	}
	flow.Description, err = localizedTextFromPayload(description)
	if err != nil {
		return flow, fmt.Errorf("scan flow description: %w", err)
	}
	return flow, nil
}

func scanFlowVersion(row postgreslib.RowScanner) (entity.FlowVersion, error) {
	var version entity.FlowVersion
	var status string
	var activatedAt pgtype.Timestamptz
	err := row.Scan(
		&version.ID,
		&version.FlowID,
		&version.Version,
		&version.SourceRef,
		&version.DefinitionDigest,
		&status,
		&activatedAt,
		&version.CreatedAt,
	)
	version.Status = enum.FlowVersionStatus(status)
	version.ActivatedAt = postgreslib.TimePtrFromPG(activatedAt)
	return version, err
}

func scanStage(row postgreslib.RowScanner) (entity.Stage, error) {
	var stage entity.Stage
	var displayName, requiredArtifacts, acceptancePolicy []byte
	var stageType string
	err := row.Scan(
		&stage.ID,
		&stage.FlowVersionID,
		&stage.Slug,
		&stageType,
		&displayName,
		&stage.IconObjectURI,
		&requiredArtifacts,
		&acceptancePolicy,
		&stage.Position,
	)
	stage.StageType = enum.StageType(stageType)
	stage.RequiredArtifactsJSON = append(stage.RequiredArtifactsJSON[:0], requiredArtifacts...)
	stage.AcceptancePolicyJSON = append(stage.AcceptancePolicyJSON[:0], acceptancePolicy...)
	if err != nil {
		return stage, err
	}
	stage.DisplayName, err = localizedTextFromPayload(displayName)
	if err != nil {
		return stage, fmt.Errorf("scan stage display_name: %w", err)
	}
	return stage, nil
}

func scanStageTransition(row postgreslib.RowScanner) (entity.StageTransition, error) {
	var transition entity.StageTransition
	var fromStageID pgtype.UUID
	var condition []byte
	err := row.Scan(
		&transition.ID,
		&transition.FlowVersionID,
		&fromStageID,
		&transition.ToStageID,
		&condition,
		&transition.FollowUpType,
		&transition.Position,
	)
	transition.FromStageID = postgreslib.UUIDPtrFromPG(fromStageID)
	transition.ConditionJSON = append(transition.ConditionJSON[:0], condition...)
	return transition, err
}

func scanStageRoleBinding(row postgreslib.RowScanner) (entity.StageRoleBinding, error) {
	var binding entity.StageRoleBinding
	var bindingKind string
	var launchPolicy []byte
	err := row.Scan(
		&binding.ID,
		&binding.StageID,
		&binding.RoleProfileID,
		&bindingKind,
		&launchPolicy,
		&binding.RequiredForAcceptance,
	)
	binding.BindingKind = enum.StageRoleBindingKind(bindingKind)
	binding.LaunchPolicyJSON = append(binding.LaunchPolicyJSON[:0], launchPolicy...)
	return binding, err
}

func scanRoleProfile(row postgreslib.RowScanner) (entity.RoleProfile, error) {
	var role entity.RoleProfile
	var displayName, allowedTools []byte
	var roleKind, status string
	err := row.Scan(
		&role.ID,
		&role.Scope.Type,
		&role.Scope.Ref,
		&role.Slug,
		&displayName,
		&role.IconObjectURI,
		&roleKind,
		&role.RuntimeProfile,
		&allowedTools,
		&role.ProviderAccountPolicyRef,
		&status,
		&role.Version,
		&role.CreatedAt,
		&role.UpdatedAt,
	)
	role.RoleKind = enum.RoleKind(roleKind)
	role.Status = enum.RoleStatus(status)
	if err != nil {
		return role, err
	}
	role.DisplayName, err = localizedTextFromPayload(displayName)
	if err != nil {
		return role, fmt.Errorf("scan role display_name: %w", err)
	}
	role.AllowedMCPTools, err = stringSliceFromPayload(allowedTools)
	if err != nil {
		return role, fmt.Errorf("scan role allowed_mcp_tools: %w", err)
	}
	return role, nil
}

func scanPromptTemplate(row postgreslib.RowScanner) (entity.PromptTemplate, error) {
	var template entity.PromptTemplate
	var promptKind string
	var activeVersionID pgtype.UUID
	err := row.Scan(
		&template.ID,
		&template.RoleProfileID,
		&promptKind,
		&activeVersionID,
		&template.Version,
		&template.CreatedAt,
		&template.UpdatedAt,
	)
	template.PromptKind = enum.PromptKind(promptKind)
	template.ActiveVersionID = postgreslib.UUIDPtrFromPG(activeVersionID)
	return template, err
}

func scanPromptTemplateVersion(row postgreslib.RowScanner) (entity.PromptTemplateVersion, error) {
	var version entity.PromptTemplateVersion
	var promptKind, status string
	var objectSize pgtype.Int8
	var activatedAt pgtype.Timestamptz
	err := row.Scan(
		&version.ID,
		&version.PromptTemplateID,
		&version.RoleProfileID,
		&promptKind,
		&version.Version,
		&version.SourceRef,
		&version.TemplateObject.ObjectURI,
		&version.TemplateObject.ObjectDigest,
		&objectSize,
		&version.TemplateDigest,
		&status,
		&activatedAt,
		&version.CreatedAt,
	)
	version.PromptKind = enum.PromptKind(promptKind)
	version.Status = enum.PromptVersionStatus(status)
	version.ActivatedAt = postgreslib.TimePtrFromPG(activatedAt)
	if objectSize.Valid {
		size := objectSize.Int64
		version.TemplateObject.ObjectSizeBytes = &size
	}
	return version, err
}

func scanCommandResult(row postgreslib.RowScanner) (entity.CommandResult, error) {
	var raw commandResultRow
	if err := raw.scan(row); err != nil {
		return entity.CommandResult{}, err
	}
	return raw.toEntity(), nil
}

func scanOutboxEvent(row postgreslib.RowScanner) (entity.OutboxEvent, error) {
	raw, err := postgreslib.ScanOutboxEventRow(row)
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	return outboxRowToEntity(raw), nil
}

type commandResultRow struct {
	result        entity.CommandResult
	commandID     pgtype.UUID
	aggregateType string
	payload       []byte
}

func (r *commandResultRow) scan(row postgreslib.RowScanner) error {
	return row.Scan(
		&r.result.Key,
		&r.commandID,
		&r.result.IdempotencyKey,
		&r.result.Actor.Type,
		&r.result.Actor.ID,
		&r.result.Operation,
		&r.aggregateType,
		&r.result.AggregateID,
		&r.payload,
		&r.result.CreatedAt,
	)
}

func (r commandResultRow) toEntity() entity.CommandResult {
	result := r.result
	result.CommandID = postgreslib.UUIDPtrFromPG(r.commandID)
	result.AggregateType = enum.CommandAggregateType(r.aggregateType)
	result.ResultPayload = append(result.ResultPayload[:0], r.payload...)
	return result
}

func outboxRowToEntity(raw postgreslib.OutboxEventRow) entity.OutboxEvent {
	event := outboxlib.NewEvent(
		raw.Identity.RowID,
		raw.Identity.TypeName,
		raw.Identity.ContractVersion,
		raw.Identity.SubjectKind,
		raw.Identity.SubjectID,
		raw.Body,
		raw.Identity.CreatedAt,
		raw.Delivery.Attempts,
	)
	return entity.OutboxEvent{
		Event:               event,
		PublishedAt:         raw.Delivery.SentAt,
		NextAttemptAt:       raw.Delivery.RetryAt,
		LockedUntil:         raw.Delivery.LeaseUntil,
		FailedPermanentlyAt: raw.Failure.DeadAt,
		FailureKind:         raw.Failure.FailureCode,
		LastError:           raw.Failure.ErrorText,
	}
}

func localizedTextFromPayload(payload []byte) ([]value.LocalizedText, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	var items []value.LocalizedText
	if err := json.Unmarshal(payload, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func stringSliceFromPayload(payload []byte) ([]string, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	var items []string
	if err := json.Unmarshal(payload, &items); err != nil {
		return nil, err
	}
	return items, nil
}
