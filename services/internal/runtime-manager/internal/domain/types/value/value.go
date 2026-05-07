// Package value contains immutable value objects used by runtime-manager.
package value

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	runtimeevents "github.com/codex-k8s/kodex/libs/go/platformevents/runtime"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
)

// Actor identifies the principal that initiated a command or read.
type Actor struct {
	Type string
	ID   string
}

// RequestContext stores safe request metadata for audit and diagnostics.
type RequestContext struct {
	Source       string
	TraceID      string
	SessionID    string
	ClientIPHash string
}

// CommandMeta carries idempotency, concurrency and audit metadata.
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

// QueryMeta carries actor and safe request metadata for reads.
type QueryMeta struct {
	Actor          Actor
	RequestID      string
	RequestContext RequestContext
}

// PageRequest limits list responses.
type PageRequest struct {
	PageSize  int32
	PageToken string
}

// PageResult returns list continuation state.
type PageResult struct {
	NextPageToken string
}

// WorkspaceSource is a normalized source prepared inside a runtime workspace.
type WorkspaceSource struct {
	SourceID      string                         `json:"source_id"`
	Kind          enum.WorkspaceSourceKind       `json:"kind"`
	RepositoryID  *uuid.UUID                     `json:"repository_id,omitempty"`
	Provider      string                         `json:"provider,omitempty"`
	ProviderOwner string                         `json:"provider_owner,omitempty"`
	ProviderName  string                         `json:"provider_name,omitempty"`
	SourceRef     string                         `json:"source_ref,omitempty"`
	CommitSHA     string                         `json:"commit_sha,omitempty"`
	LocalPath     string                         `json:"local_path"`
	AccessMode    enum.WorkspaceSourceAccessMode `json:"access_mode"`
	Digest        string                         `json:"digest,omitempty"`
	Metadata      json.RawMessage                `json:"metadata_json,omitempty"`
}

// RuntimeEventPayload is generated from AsyncAPI and used by runtime events.
type RuntimeEventPayload = runtimeevents.Payload
