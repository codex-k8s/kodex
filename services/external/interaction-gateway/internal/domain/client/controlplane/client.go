// Package controlplane задаёт специализированные owner RPC interaction-gateway.
package controlplane

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
)

var ErrConflict = errors.New("control-plane command conflict")

var (
	ErrNotFound    = errors.New("control-plane resource is not found")
	ErrUnavailable = errors.New("control-plane working path is unavailable")
)

type Artifact struct {
	ID         string
	Version    uint64
	ScanState  string
	Name       string
	Direction  string
	StorageRef string
	SizeBytes  uint64
	MediaType  string
	SHA256     string
}

type Session struct {
	ID      string
	Version uint64
}

type Turn struct {
	ID                    string
	Version               uint64
	State                 string
	SessionID             string
	Attempt               uint32
	Outcome               string
	ResultArtifactID      string
	ResultArtifactVersion uint64
	ResultArtifactSHA256  string
	ImmutableInputSHA256  string
}

type ArtifactInput struct {
	IdempotencyKey string
	Name           string
	ParentID       string
	Kind           string
	Direction      string
	StorageRef     string
	SizeBytes      uint64
	MediaType      string
	SHA256         string
	RetentionRef   string
}

type ResolveGateInput struct {
	IdempotencyKey       string
	GateID               string
	GateVersion          uint64
	Decision             string
	Reason               string
	ProcessRunID         string
	ProcessVersion       uint64
	SessionID            string
	TurnID               string
	Attempt              uint32
	ImmutableInputSHA256 string
}

type RuntimeActionInput struct {
	IdempotencyKey string
	SessionID      string
	TurnID         string
	Action         string
}

type SessionMCPBindingInput struct {
	IdempotencyKey, SessionID, AgentSessionKey, AgentSessionBindingSHA256 string
	AgentSessionVersion                                                   uint64
	AgentSessionID                                                        int64
	ImmutableSecretRef, ProviderContentVersion, ContentSHA256             string
}

type RecordDeliveryInput struct {
	IdempotencyKey        string
	ProjectID             string
	GateID                string
	GateVersion           uint64
	DeliveryID            string
	PayloadSHA256         string
	ClaimToken            string
	ClaimFence            uint64
	PostID                string
	ChannelID             string
	RootPostID            string
	ProviderReceiptSHA256 string
}

type InteractionDeliveryWork struct {
	DeliveryID, OrganizationID, ProjectID, ActorID           string
	SessionID                                                string
	SessionVersion                                           uint64
	TurnID                                                   string
	TurnVersion                                              uint64
	Attempt                                                  uint32
	RuntimeRevisionID                                        string
	RuntimeRevisionVersion                                   uint64
	ImmutableInputSHA256                                     string
	Kind, LifecycleState, Outcome                            string
	ArtifactID                                               string
	ArtifactVersion                                          uint64
	ArtifactSHA256                                           string
	ArtifactName, ArtifactStorageRef, ArtifactMediaType      string
	ArtifactSizeBytes                                        uint64
	Fence                                                    uint64
	LeaseToken                                               string
	LeaseExpiresAt                                           time.Time
	ReadbackCredential                                       string
	InlinePayload                                            []byte
	NotificationRoomID, NotificationPolicy, ScheduledOutcome string
}

type RuntimeMaterialization struct {
	ProjectID, StorageRef, MediaType, SHA256 string
	ArtifactVersion, SizeBytes               uint64
}

type RuntimeOutputMetadata struct {
	Kind, Name, MediaType, SHA256 string
	SizeBytes                     uint64
	Sequence, Total               uint32
}

type RuntimeOutputAuthorization struct {
	OrganizationID, ProjectID string
	ExecutionVersion, Fence   uint64
	GrantGeneration           uint64
}

type ProviderEffectReceipt struct {
	ContractVersion          uint32    `json:"contract_version"`
	Issuer                   string    `json:"iss"`
	Audience                 string    `json:"aud"`
	Purpose                  string    `json:"purpose"`
	WorkloadID               string    `json:"workload_id"`
	CallerSPIFFEID           string    `json:"caller_spiffe_id"`
	FullMethod               string    `json:"full_method"`
	ActorID                  string    `json:"actor_id"`
	OrganizationID           string    `json:"organization_id"`
	ProjectID                string    `json:"project_id"`
	WorkspaceID              string    `json:"workspace_id,omitempty"`
	ProviderTeamRef          string    `json:"provider_team_ref,omitempty"`
	ProviderObjectRef        string    `json:"provider_object_ref,omitempty"`
	Action                   string    `json:"action"`
	Effect                   string    `json:"effect"`
	EffectVersion            uint64    `json:"effect_version"`
	EffectGeneration         uint64    `json:"effect_generation"`
	EffectSHA256             string    `json:"effect_sha256"`
	ReceiptID                string    `json:"jti"`
	ReceiptRevision          uint64    `json:"revision"`
	IssuedAt                 time.Time `json:"issued_at"`
	NotBefore                time.Time `json:"not_before"`
	ExpiresAt                time.Time `json:"expires_at"`
	MaskedStatus             string    `json:"masked_status"`
	Eligible                 bool      `json:"eligible"`
	TargetKind               string    `json:"target_kind"`
	TargetResourceID         string    `json:"target_resource_id,omitempty"`
	TargetStableKey          string    `json:"target_stable_key"`
	CommandIntentSHA256      string    `json:"command_intent_sha256"`
	CredentialBindingID      string    `json:"credential_binding_id,omitempty"`
	CredentialBindingVersion uint64    `json:"credential_binding_version,omitempty"`
	CredentialBindingSHA256  string    `json:"credential_binding_sha256,omitempty"`
	ProviderUsername         string    `json:"provider_username,omitempty"`
	Provider                 string    `json:"provider,omitempty"`
	MaskedLabel              string    `json:"masked_label,omitempty"`
	Capabilities             []string  `json:"capabilities,omitempty"`
}

type ProviderCredential struct {
	CompactJWS string
	Receipt    ProviderEffectReceipt
}

type ManageWorkspaceMappingInput struct {
	IdempotencyKey     string
	Action             string
	MappingID          string
	ExpectedVersion    uint64
	ExpectedGeneration uint64
	Name               string
	Credential         ProviderCredential
}

type AgentMattermostBotOwner struct {
	AgentRef              string
	AgentStableKey        string
	AgentVersion          uint64
	BotIdentityRef        string
	BotUsername           string
	BotProviderRevision   uint64
	BotProviderGeneration uint64
	BotProviderTeamRef    string
	BotMaskedStatus       string
	BotReceiptID          string
	BotReceiptSHA256      string
	BotReceiptVersion     uint64
}

type ManageAgentMattermostBotIdentityInput struct {
	IdempotencyKey  string
	Action          string
	AgentRef        string
	ExpectedVersion uint64
	Readiness       bool
	Credential      ProviderCredential
}

type Client interface {
	Check(context.Context) error
	CheckInteraction(context.Context, string, string) error
	RegisterArtifact(context.Context, string, ArtifactInput) (Artifact, error)
	GetArtifact(context.Context, string, string, uint64) (Artifact, error)
	CreateSession(context.Context, string, string, string, string, string) (Session, error)
	BindSessionMCP(context.Context, string, SessionMCPBindingInput) (Session, error)
	EnqueueTurn(context.Context, string, string, string, string, string, []string) (Turn, error)
	GetTurn(context.Context, string, string) (Turn, error)
	ManageConversationLifecycle(context.Context, string, string, string, string, string) error
	ClaimOwnerGate(context.Context, string) (entity.OwnerGateClaim, error)
	RecordOwnerGateDelivery(context.Context, string, RecordDeliveryInput) error
	ResolveOwnerGate(context.Context, string, ResolveGateInput) error
	ManageRuntimeAction(context.Context, string, RuntimeActionInput) (Turn, error)
	ExpireOwnerGate(context.Context, string) error
	ClaimInteractionDelivery(context.Context, string) (InteractionDeliveryWork, error)
	RecordInteractionDelivery(context.Context, string, InteractionDeliveryWork, string) error
	IssueInteractionDeliveryReadback(context.Context, string, string, string, bool) (string, time.Time, error)
	ValidateInteractionDeliveryReadback(context.Context, string, string, string, string, string, string, uint64) (bool, error)
	GetRuntimeMaterialization(context.Context, string, string, string, uint64, string) (RuntimeMaterialization, error)
	AuthorizeRuntimeOutput(context.Context, string, string, RuntimeOutputMetadata) (RuntimeOutputAuthorization, error)
	RegisterRuntimeOutput(context.Context, string, string, RuntimeOutputAuthorization, RuntimeOutputMetadata, string) (Artifact, error)
	ListWorkspaceMattermostMappings(context.Context, ProviderCredential, string) ([]entity.WorkspaceMattermostMapping, error)
	GetWorkspaceMattermostMapping(context.Context, ProviderCredential, string) (entity.WorkspaceMattermostMapping, error)
	ManageWorkspaceMattermostMapping(context.Context, ManageWorkspaceMappingInput) (entity.WorkspaceMattermostMapping, error)
	GetAgentMattermostBotIdentity(context.Context, ProviderCredential, string) (AgentMattermostBotOwner, error)
	ManageAgentMattermostBotIdentity(context.Context, ManageAgentMattermostBotIdentityInput) (AgentMattermostBotOwner, error)
}
