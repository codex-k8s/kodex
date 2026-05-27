package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	agentevents "github.com/codex-k8s/kodex/libs/go/platformevents/agent"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
)

func flowActivatedEvent(id uuid.UUID, flow entity.Flow, version entity.FlowVersion, occurredAt time.Time) (entity.OutboxEvent, error) {
	payload, err := json.Marshal(agentevents.Payload{
		FlowID:           version.FlowID.String(),
		FlowVersionID:    version.ID.String(),
		ActivatedVersion: version.Version,
		Version:          flow.Version,
	})
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	return outboxEvent(id, agentevents.EventFlowVersionActivated, agentevents.AggregateFlow, version.FlowID, payload, occurredAt), nil
}

func roleActivatedEvent(id uuid.UUID, role entity.RoleProfile, occurredAt time.Time) (entity.OutboxEvent, error) {
	digest, err := roleProfileDigest(role)
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	payload, err := json.Marshal(agentevents.Payload{
		RoleProfileID:      role.ID.String(),
		RoleProfileVersion: role.Version,
		RoleProfileDigest:  digest,
		Version:            role.Version,
	})
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	return outboxEvent(id, agentevents.EventRoleVersionActivated, agentevents.AggregateRole, role.ID, payload, occurredAt), nil
}

func promptActivatedEvent(id uuid.UUID, template entity.PromptTemplate, version entity.PromptTemplateVersion, occurredAt time.Time) (entity.OutboxEvent, error) {
	payload, err := json.Marshal(agentevents.Payload{
		RoleProfileID:           version.RoleProfileID.String(),
		PromptTemplateVersionID: version.ID.String(),
		PromptTemplateDigest:    version.TemplateDigest,
		ActivatedVersion:        version.Version,
		Version:                 template.Version,
	})
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	return outboxEvent(id, agentevents.EventPromptVersionActivated, agentevents.AggregatePrompt, version.PromptTemplateID, payload, occurredAt), nil
}

func sessionCreatedEvent(id uuid.UUID, session entity.AgentSession, occurredAt time.Time) (entity.OutboxEvent, error) {
	return sessionEvent(id, agentevents.EventSessionCreated, session, occurredAt)
}

func sessionEvent(id uuid.UUID, eventType string, session entity.AgentSession, occurredAt time.Time) (entity.OutboxEvent, error) {
	payload, err := json.Marshal(agentevents.Payload{
		SessionID:           session.ID.String(),
		FlowVersionID:       optionalUUIDValue(session.FlowVersionID),
		StageID:             optionalUUIDValue(session.CurrentStageID),
		ProviderWorkItemRef: session.ProviderWorkItemRef,
		Status:              string(session.Status),
		Version:             session.Version,
	})
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	return outboxEvent(id, eventType, agentevents.AggregateSession, session.ID, payload, occurredAt), nil
}

func runRequestedEvent(id uuid.UUID, run entity.AgentRun, occurredAt time.Time) (entity.OutboxEvent, error) {
	return runEvent(id, agentevents.EventRunRequested, "", "", run, occurredAt)
}

func runStateEvent(id uuid.UUID, previousStatus string, run entity.AgentRun, reasonCode string, occurredAt time.Time) (*entity.OutboxEvent, error) {
	if previousStatus == string(run.Status) {
		return nil, nil
	}
	var eventType string
	switch string(run.Status) {
	case "starting", "running":
		eventType = agentevents.EventRunStarted
	case "waiting":
		eventType = agentevents.EventRunWaiting
	case "completed":
		eventType = agentevents.EventRunCompleted
	case "failed":
		eventType = agentevents.EventRunFailed
	default:
		return nil, nil
	}
	event, err := runEvent(id, eventType, previousStatus, reasonCode, run, occurredAt)
	if err != nil {
		return nil, err
	}
	return &event, nil
}

func runEvent(id uuid.UUID, eventType string, previousStatus string, reasonCode string, run entity.AgentRun, occurredAt time.Time) (entity.OutboxEvent, error) {
	payload, err := json.Marshal(agentevents.Payload{
		SessionID:               run.SessionID.String(),
		RunID:                   run.ID.String(),
		FlowVersionID:           optionalUUIDValue(run.FlowVersionID),
		StageID:                 optionalUUIDValue(run.StageID),
		RoleProfileID:           run.RoleProfileID.String(),
		RoleProfileVersion:      run.RoleProfileVersion,
		RoleProfileDigest:       run.RoleProfileDigest,
		PromptTemplateVersionID: run.PromptTemplateVersionID.String(),
		PromptTemplateDigest:    run.PromptTemplateDigest,
		GuidanceRefCount:        int64(len(run.GuidanceRefs)),
		RuntimeSlotRef:          run.RuntimeContext.SlotRef,
		RuntimeJobRef:           run.RuntimeContext.JobRef,
		ProviderWorkItemRef:     run.ProviderTarget.WorkItemRef,
		ProviderPullRequestRef:  run.ProviderTarget.PullRequestRef,
		PreviousStatus:          previousStatus,
		Status:                  string(run.Status),
		ReasonCode:              reasonCode,
		FailureCode:             run.FailureCode,
		Version:                 run.Version,
	})
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	return outboxEvent(id, eventType, agentevents.AggregateRun, run.ID, payload, occurredAt), nil
}

func sessionSnapshotRecordedEvent(id uuid.UUID, snapshot entity.AgentSessionStateSnapshot, session entity.AgentSession, occurredAt time.Time) (entity.OutboxEvent, error) {
	payload, err := json.Marshal(agentevents.Payload{
		SessionID:           session.ID.String(),
		RunID:               optionalUUIDValue(snapshot.RunID),
		StateSnapshotID:     snapshot.ID.String(),
		StateSnapshotDigest: snapshot.Object.ObjectDigest,
		Version:             session.Version,
	})
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	return outboxEvent(id, agentevents.EventSessionSnapshotRecorded, agentevents.AggregateSession, session.ID, payload, occurredAt), nil
}

func acceptanceRequestedEvent(id uuid.UUID, acceptance entity.AcceptanceResult, occurredAt time.Time) (entity.OutboxEvent, error) {
	return acceptanceEvent(id, agentevents.EventAcceptanceRequested, "", acceptance, occurredAt)
}

func acceptanceResultEvent(id uuid.UUID, previousStatus string, acceptance entity.AcceptanceResult, occurredAt time.Time) (*entity.OutboxEvent, error) {
	if previousStatus == string(acceptance.Status) {
		return nil, nil
	}
	eventType := ""
	reasonCode := ""
	switch acceptance.Status {
	case enum.AcceptanceStatusPassed, enum.AcceptanceStatusSkipped:
		eventType = agentevents.EventAcceptanceCompleted
	case enum.AcceptanceStatusFailed:
		eventType = agentevents.EventAcceptanceFailed
		reasonCode = acceptanceFailureReasonCode
	default:
		return nil, nil
	}
	event, err := acceptanceEvent(id, eventType, reasonCode, acceptance, occurredAt)
	if err != nil {
		return nil, err
	}
	return &event, nil
}

func acceptanceEvent(id uuid.UUID, eventType string, reasonCode string, acceptance entity.AcceptanceResult, occurredAt time.Time) (entity.OutboxEvent, error) {
	payload, err := json.Marshal(agentevents.Payload{
		SessionID:          acceptance.SessionID.String(),
		RunID:              optionalUUIDValue(acceptance.RunID),
		StageID:            optionalUUIDValue(acceptance.StageID),
		AcceptanceResultID: acceptance.ID.String(),
		Status:             string(acceptance.Status),
		ReasonCode:         reasonCode,
		Version:            acceptance.Version,
	})
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	return outboxEvent(id, eventType, agentevents.AggregateAcceptance, acceptance.ID, payload, occurredAt), nil
}

func followUpRequestedEvent(id uuid.UUID, intent entity.FollowUpIntent, occurredAt time.Time) (entity.OutboxEvent, error) {
	return followUpEvent(id, agentevents.EventFollowUpRequested, "", intent, occurredAt)
}

func followUpResultEvent(id uuid.UUID, previousStatus string, intent entity.FollowUpIntent, occurredAt time.Time) (*entity.OutboxEvent, error) {
	if previousStatus == string(intent.Status) {
		return nil, nil
	}
	var eventType string
	var reasonCode string
	switch intent.Status {
	case enum.FollowUpIntentStatusCreated:
		eventType = agentevents.EventFollowUpCreated
	case enum.FollowUpIntentStatusUpdated:
		eventType = agentevents.EventFollowUpUpdated
	case enum.FollowUpIntentStatusCommented:
		eventType = agentevents.EventFollowUpCommented
	case enum.FollowUpIntentStatusReviewSignaled:
		eventType = agentevents.EventFollowUpReviewSignaled
	case enum.FollowUpIntentStatusFailed:
		eventType = agentevents.EventFollowUpFailed
		reasonCode = "provider_command_failed"
	default:
		return nil, nil
	}
	event, err := followUpEvent(id, eventType, reasonCode, intent, occurredAt)
	if err != nil {
		return nil, err
	}
	return &event, nil
}

func followUpEvent(id uuid.UUID, eventType string, reasonCode string, intent entity.FollowUpIntent, occurredAt time.Time) (entity.OutboxEvent, error) {
	payload, err := json.Marshal(agentevents.Payload{
		SessionID:               intent.SessionID.String(),
		RunID:                   optionalUUIDValue(intent.RunID),
		FromStageID:             optionalUUIDValue(intent.FromStageID),
		ToStageID:               optionalUUIDValue(intent.ToStageID),
		StageID:                 optionalUUIDValue(intent.ToStageID),
		AcceptanceResultID:      optionalUUIDValue(intent.AcceptanceResultID),
		FollowUpIntentID:        intent.ID.String(),
		ProviderWorkItemRef:     intent.ProviderTarget.WorkItemRef,
		ProviderPullRequestRef:  intent.ProviderTarget.PullRequestRef,
		ProviderCommentRef:      intent.ProviderTarget.CommentRef,
		ProviderReviewSignalRef: intent.ProviderTarget.ReviewSignalRef,
		ProviderWorkItemType:    intent.ProviderWorkItemType,
		ProviderOperationRef:    intent.ProviderOperationRef,
		Status:                  string(intent.Status),
		Summary:                 intent.SafeSummary,
		ReasonCode:              reasonCode,
		FailureCode:             reasonCode,
		Version:                 intent.Version,
	})
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	return outboxEvent(id, eventType, agentevents.AggregateFollowUp, intent.ID, payload, occurredAt), nil
}

func optionalUUIDValue(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func outboxEvent(id uuid.UUID, eventType string, aggregateType string, aggregateID uuid.UUID, payload []byte, occurredAt time.Time) entity.OutboxEvent {
	return entity.OutboxEvent{
		Event: outboxlib.NewEvent(
			id,
			eventType,
			agentevents.SchemaVersion,
			aggregateType,
			aggregateID,
			payload,
			occurredAt,
			0,
		),
		NextAttemptAt: occurredAt,
	}
}

func roleProfileDigest(role entity.RoleProfile) (string, error) {
	payload, err := json.Marshal(struct {
		ID                       string   `json:"id"`
		RoleKind                 string   `json:"role_kind"`
		RuntimeProfile           string   `json:"runtime_profile"`
		AllowedMCPTools          []string `json:"allowed_mcp_tools"`
		ProviderAccountPolicyRef string   `json:"provider_account_policy_ref,omitempty"`
		Version                  int64    `json:"version"`
	}{
		ID:                       role.ID.String(),
		RoleKind:                 string(role.RoleKind),
		RuntimeProfile:           role.RuntimeProfile,
		AllowedMCPTools:          role.AllowedMCPTools,
		ProviderAccountPolicyRef: role.ProviderAccountPolicyRef,
		Version:                  role.Version,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
