// Package value contains immutable value objects used by runtime-manager.
package value

import (
	"time"

	"github.com/google/uuid"

	runtimeevents "github.com/codex-k8s/kodex/libs/go/platformevents/runtime"
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

// RuntimeEventPayload is generated from AsyncAPI and used by runtime events.
type RuntimeEventPayload = runtimeevents.Payload
