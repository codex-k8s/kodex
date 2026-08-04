// Package controlplane задаёт специализированные owner RPC interaction-gateway.
package controlplane

import (
	"context"
	"errors"

	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
)

var ErrConflict = errors.New("control-plane command conflict")

type Artifact struct {
	ID        string
	Version   uint64
	ScanState string
}

type Session struct {
	ID      string
	Version uint64
}

type Turn struct {
	ID      string
	Version uint64
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

type Client interface {
	Check(context.Context) error
	RegisterArtifact(context.Context, string, ArtifactInput) (Artifact, error)
	GetArtifact(context.Context, string, string, uint64) (Artifact, error)
	CreateSession(context.Context, string, string, string, string, string) (Session, error)
	EnqueueTurn(context.Context, string, string, string, string, string) (Turn, error)
	ClaimOwnerGate(context.Context, string) (entity.OwnerGateClaim, error)
	RecordOwnerGateDelivery(context.Context, string, RecordDeliveryInput) error
	ResolveOwnerGate(context.Context, string, ResolveGateInput) error
	ExpireOwnerGate(context.Context, string) error
}
