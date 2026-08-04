// Package controlplane задаёт специализированные owner RPC interaction-gateway.
package controlplane

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
)

var ErrConflict = errors.New("control-plane command conflict")

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
	IdempotencyKey string
	GateID         string
	GateVersion    uint64
	DeliveryID     string
	PayloadSHA256  string
	ClaimToken     string
	ClaimFence     uint64
	PostID         string
	ChannelID      string
	RootPostID     string
}

type InteractionDeliveryWork struct {
	DeliveryID, OrganizationID, ProjectID, ActorID      string
	SessionID                                           string
	SessionVersion                                      uint64
	TurnID                                              string
	TurnVersion                                         uint64
	Attempt                                             uint32
	RuntimeRevisionID                                   string
	RuntimeRevisionVersion                              uint64
	ImmutableInputSHA256                                string
	Kind, LifecycleState, Outcome                       string
	ArtifactID                                          string
	ArtifactVersion                                     uint64
	ArtifactSHA256                                      string
	ArtifactName, ArtifactStorageRef, ArtifactMediaType string
	ArtifactSizeBytes                                   uint64
	Fence                                               uint64
	LeaseToken                                          string
	LeaseExpiresAt                                      time.Time
	ReadbackCredential                                  string
	InlinePayload                                       []byte
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
	IssueInteractionDeliveryReadback(context.Context, string, string, bool) (string, time.Time, error)
	ValidateInteractionDeliveryReadback(context.Context, string, string, string, string, string, string, uint64) (bool, error)
	GetRuntimeMaterialization(context.Context, string, string, string, uint64, string) (RuntimeMaterialization, error)
	AuthorizeRuntimeOutput(context.Context, string, string, RuntimeOutputMetadata) (RuntimeOutputAuthorization, error)
	RegisterRuntimeOutput(context.Context, string, string, RuntimeOutputAuthorization, RuntimeOutputMetadata, string) (Artifact, error)
}
