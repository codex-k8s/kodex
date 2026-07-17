package security

import (
	"context"
	"errors"
	"time"
)

var (
	ErrCapabilityNotFound = errors.New("interaction capability not found")
	ErrCapabilityExpired  = errors.New("interaction capability expired")
	ErrCapabilityConsumed = errors.New("interaction capability consumed")
	ErrCapabilityBinding  = errors.New("interaction capability binding mismatch")
	ErrCapabilityInactive = errors.New("interaction capability inactive")
)

type CapabilityState string

const (
	CapabilityStatePending  CapabilityState = "pending"
	CapabilityStateUnused   CapabilityState = "unused"
	CapabilityStateConsumed CapabilityState = "consumed"
	CapabilityStateRevoked  CapabilityState = "revoked"
)

type Capability struct {
	State             CapabilityState
	Kind              string
	Operation         string
	ResourceType      string
	ResourceID        string
	ChannelID         string
	PostBinding       string
	ActorUserID       string
	ActorUserName     string
	InstallationScope string
	WorkspaceScope    string
	SessionScope      string
	IssuedAt          time.Time
	ExpiresAt         time.Time
	ConsumedAt        time.Time
}

type IssueCapabilityInput struct {
	TokenHash         []byte
	Kind              string
	Operation         string
	ResourceType      string
	ResourceID        string
	ChannelID         string
	PostBinding       string
	ActorUserID       string
	ActorUserName     string
	InstallationScope string
	WorkspaceScope    string
	SessionScope      string
	ContextHash       []byte
	IssuedAt          time.Time
	ExpiresAt         time.Time
	State             CapabilityState
}

type ConsumeCapabilityInput struct {
	TokenHash    []byte
	Kind         string
	Operation    string
	ResourceType string
	ResourceID   string
	ChannelID    string
	PostBinding  string
	ActorUserID  string
	ContextHash  []byte
	Now          time.Time
}

type TransitionCapabilitiesInput struct {
	TokenHashes [][]byte
	From        CapabilityState
	To          CapabilityState
}

type InteractionResourceAdmissionInput struct {
	ActionKey    string
	Operation    string
	ResourceType string
	ResourceID   string
	ActorUserID  string
	ChannelID    string
	PostID       string
	Installation string
	Workspace    string
	Session      string
}

type ClusterAdminBindingInput struct {
	RoleID              int64
	ProjectID           int64
	ChatID              int64
	ChatSlug            string
	MattermostChannelID string
	SessionKey          string
	Operation           string
	ActorUserID         string
	ActorUser           string
}

type CapabilityCleanupInput struct {
	DeleteBefore time.Time
	Limit        int
}

type ClusterAdminAdmissionInput struct {
	SubjectType string
	SubjectKey  string
	ProjectID   int64
	ProfileName string
	ActorUserID string
	ActorUser   string
	Operation   string
}

type Repository interface {
	IssueInteractionCapability(ctx context.Context, input IssueCapabilityInput) error
	CheckInteractionCapability(ctx context.Context, input ConsumeCapabilityInput) (Capability, error)
	ConsumeInteractionCapability(ctx context.Context, input ConsumeCapabilityInput) (Capability, error)
	TransitionInteractionCapabilities(ctx context.Context, input TransitionCapabilitiesInput) error
	AdmitExistingClusterAdmin(ctx context.Context, input ClusterAdminAdmissionInput) (bool, error)
}

type InteractionResourceAdmissionRepository interface {
	AdmitInteractionResource(ctx context.Context, input InteractionResourceAdmissionInput) (bool, error)
}

type ClusterAdminBindingRepository interface {
	AdmitExistingClusterAdminBinding(ctx context.Context, input ClusterAdminBindingInput) (bool, error)
}

type ClusterAdminRuntimeGuardRepository interface {
	WithExistingClusterAdminRuntimeGuard(ctx context.Context, input ClusterAdminBindingInput, sideEffect func() error) error
}

type CapabilityCleanupRepository interface {
	CleanupInteractionCapabilities(ctx context.Context, input CapabilityCleanupInput) (int64, error)
}
