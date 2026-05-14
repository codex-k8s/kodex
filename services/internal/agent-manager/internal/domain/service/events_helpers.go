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
)

func flowActivatedEvent(id uuid.UUID, version entity.FlowVersion, occurredAt time.Time) (entity.OutboxEvent, error) {
	payload, err := json.Marshal(agentevents.Payload{
		FlowID:           version.FlowID.String(),
		FlowVersionID:    version.ID.String(),
		ActivatedVersion: version.Version,
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
	})
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	return outboxEvent(id, agentevents.EventRoleVersionActivated, agentevents.AggregateRole, role.ID, payload, occurredAt), nil
}

func promptActivatedEvent(id uuid.UUID, version entity.PromptTemplateVersion, occurredAt time.Time) (entity.OutboxEvent, error) {
	payload, err := json.Marshal(agentevents.Payload{
		PromptTemplateVersionID: version.ID.String(),
		PromptTemplateDigest:    version.TemplateDigest,
		Version:                 version.Version,
	})
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	return outboxEvent(id, agentevents.EventPromptVersionActivated, agentevents.AggregatePrompt, version.PromptTemplateID, payload, occurredAt), nil
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
