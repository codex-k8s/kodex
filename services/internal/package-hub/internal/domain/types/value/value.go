// Package value contains package-hub value objects.
package value

import (
	"time"

	"github.com/google/uuid"

	packageevents "github.com/codex-k8s/kodex/libs/go/platformevents/packagehub"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/enum"
)

type Actor struct {
	Type string
	ID   string
}

type RequestContext struct {
	Source       string
	TraceID      string
	SessionID    string
	ClientIPHash string
}

type CommandMeta struct {
	CommandID       uuid.UUID
	IdempotencyKey  string
	ExpectedVersion *int64
	Actor           Actor
	Reason          string
	RequestID       string
	RequestContext  RequestContext
	OccurredAt      time.Time
}

type QueryMeta struct {
	Actor          Actor
	RequestID      string
	RequestContext RequestContext
}

type LocalizedText struct {
	Locale string `json:"locale"`
	Text   string `json:"text"`
}

type SourceRef struct {
	Kind      enum.PackageVersionSourceRefKind
	Ref       string
	CommitSHA string
}

type ScopeRef struct {
	Type enum.PackageInstallationScopeType
	Ref  string
}

type PackageSecretField struct {
	Key         string                      `json:"key"`
	Kind        enum.PackageSecretFieldKind `json:"kind"`
	Required    bool                        `json:"required"`
	DisplayName []LocalizedText             `json:"display_name"`
	Description []LocalizedText             `json:"description"`
}

type PageRequest struct {
	PageSize  int32
	PageToken string
}

type PageResult struct {
	NextPageToken string
}

type PackageEventPayload = packageevents.Payload
