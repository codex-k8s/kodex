// Package entity содержит gateway-owned transport/idempotency state.
package entity

import (
	"encoding/json"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/enum"
)

type InboundEvent struct {
	ID                  string             `json:"id"`
	ProviderEventID     string             `json:"provider_event_id"`
	Kind                enum.InboundKind   `json:"kind"`
	Revision            uint64             `json:"revision"`
	TeamID              string             `json:"team_id"`
	ChannelID           string             `json:"channel_id"`
	PostID              string             `json:"post_id"`
	RootPostID          string             `json:"root_post_id,omitempty"`
	UserID              string             `json:"user_id"`
	Text                string             `json:"text"`
	FileIDs             []string           `json:"file_ids,omitempty"`
	Action              enum.OwnerDecision `json:"action,omitempty"`
	ActionReason        string             `json:"action_reason,omitempty"`
	DeliveryID          string             `json:"delivery_id,omitempty"`
	CallbackToken       string             `json:"callback_token,omitempty"`
	OrganizationID      string             `json:"organization_id"`
	ProjectID           string             `json:"project_id"`
	ChatID              string             `json:"chat_id"`
	ActorID             string             `json:"actor_id"`
	RoleID              string             `json:"role_id"`
	Locale              string             `json:"locale"`
	BotStableKey        string             `json:"bot_stable_key"`
	DigestSHA256        string             `json:"digest_sha256"`
	SessionID           string             `json:"session_id,omitempty"`
	PromptArtifactID    string             `json:"prompt_artifact_id,omitempty"`
	AttachmentArtifacts []ArtifactBinding  `json:"attachment_artifacts,omitempty"`
	State               enum.InboundState  `json:"state"`
	Attempts            uint32             `json:"attempts"`
	NextAttemptAt       time.Time          `json:"next_attempt_at"`
	CreatedAt           time.Time          `json:"created_at"`
	UpdatedAt           time.Time          `json:"updated_at"`
}

type ArtifactBinding struct {
	ArtifactID string `json:"artifact_id,omitempty"`
	Version    uint64 `json:"version,omitempty"`
	FileID     string `json:"file_id,omitempty"`
	Name       string `json:"name"`
	Path       string `json:"path"`
	StorageRef string `json:"storage_ref"`
	SizeBytes  uint64 `json:"size_bytes"`
	MediaType  string `json:"media_type"`
	SHA256     string `json:"sha256"`
	Provenance string `json:"provenance"`
	ScanState  string `json:"scan_state,omitempty"`
}

type Delivery struct {
	ID                    string             `json:"delivery_id"`
	Kind                  enum.DeliveryKind  `json:"kind"`
	State                 enum.DeliveryState `json:"state"`
	OrganizationID        string             `json:"organization_id"`
	ProjectID             string             `json:"project_id"`
	SessionID             string             `json:"session_id,omitempty"`
	TurnID                string             `json:"turn_id,omitempty"`
	Attempt               uint32             `json:"attempt,omitempty"`
	ImmutableInputSHA256  string             `json:"input_sha256,omitempty"`
	TeamID                string             `json:"team_id"`
	ChannelID             string             `json:"channel_id"`
	RootPostID            string             `json:"root_post_id,omitempty"`
	BotStableKey          string             `json:"bot_stable_key"`
	Locale                string             `json:"locale"`
	Payload               json.RawMessage    `json:"payload"`
	PayloadSHA256         string             `json:"payload_sha256"`
	Attachments           []ArtifactBinding  `json:"attachments,omitempty"`
	ProviderPostID        string             `json:"provider_post_id,omitempty"`
	ProviderReceiptSHA256 string             `json:"provider_receipt_sha256,omitempty"`
	Attempts              uint32             `json:"attempts"`
	Fence                 uint64             `json:"fence"`
	LeaseToken            string             `json:"-"`
	LeaseExpiresAt        time.Time          `json:"lease_expires_at,omitempty"`
	NextAttemptAt         time.Time          `json:"next_attempt_at"`
	LastErrorCode         string             `json:"last_error_code,omitempty"`
	OwnerGate             *OwnerGateBinding  `json:"owner_gate,omitempty"`
	CreatedAt             time.Time          `json:"created_at"`
	UpdatedAt             time.Time          `json:"updated_at"`
}

type OwnerGateBinding struct {
	GateID                string    `json:"gate_id"`
	GateVersion           uint64    `json:"gate_version"`
	ProcessRunID          string    `json:"process_run_id"`
	ProcessVersion        uint64    `json:"process_version"`
	ClaimToken            string    `json:"-"`
	ClaimFence            uint64    `json:"claim_fence"`
	ClaimExpiresAt        time.Time `json:"claim_expires_at"`
	RecipientActorID      string    `json:"recipient_actor_id"`
	DeliveryPayloadSHA256 string    `json:"delivery_payload_sha256"`
	DeliveryRecordedAt    time.Time `json:"delivery_recorded_at,omitempty"`
}

type Boundary struct {
	OrganizationID string
	ProjectID      string
	ChatID         string
	ActorID        string
	RoleID         string
	Locale         string
	BotStableKey   string
	TeamID         string
	ChannelID      string
	SessionID      string
	IgnoredBot     bool
}

type OwnerGateClaim struct {
	DeliveryID            string
	DeliveryPayloadSHA256 string
	GateID                string
	GateVersion           uint64
	ProjectID             string
	ProcessRunID          string
	ProcessVersion        uint64
	SessionID             string
	TurnID                string
	Attempt               uint32
	ImmutableInputSHA256  string
	RecipientActorID      string
	ClaimToken            string
	ClaimFence            uint64
	ClaimExpiresAt        time.Time
	ResultRef             string
	Summary               string
}

type WorkspaceManifest struct {
	Version         int               `json:"version"`
	OrganizationID  string            `json:"organization_id"`
	ProjectID       string            `json:"project_id"`
	ChatID          string            `json:"chat_id"`
	SessionID       string            `json:"session_id"`
	ProviderEventID string            `json:"provider_event_id"`
	Prompt          string            `json:"prompt"`
	PromptSHA256    string            `json:"prompt_sha256"`
	Files           []ArtifactBinding `json:"files"`
}
