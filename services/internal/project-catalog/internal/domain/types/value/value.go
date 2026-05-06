// Package value contains immutable value objects used by project-catalog.
package value

import (
	"time"

	"github.com/google/uuid"

	projectevents "github.com/codex-k8s/kodex/libs/go/platformevents/project"
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

// ProjectEventPayload is generated from AsyncAPI and used by project events.
type ProjectEventPayload = projectevents.Payload

// PolicyEditProposalRequestedChanges is the typed request to change services.yaml through PR.
type PolicyEditProposalRequestedChanges struct {
	Summary string                     `json:"summary,omitempty"`
	Changes []PolicyEditProposalChange `json:"changes,omitempty"`
}

// PolicyEditProposalChange describes one requested services.yaml change.
type PolicyEditProposalChange struct {
	Type        PolicyEditProposalChangeType `json:"type"`
	Target      string                       `json:"target,omitempty"`
	Description string                       `json:"description,omitempty"`
}

// PolicyEditProposalChangeType classifies supported services.yaml edit intents.
type PolicyEditProposalChangeType string

const (
	PolicyEditProposalChangeAddService          PolicyEditProposalChangeType = "add_service"
	PolicyEditProposalChangeUpdateService       PolicyEditProposalChangeType = "update_service"
	PolicyEditProposalChangeRemoveService       PolicyEditProposalChangeType = "remove_service"
	PolicyEditProposalChangeAddDocumentation    PolicyEditProposalChangeType = "add_documentation_source"
	PolicyEditProposalChangeUpdateDocumentation PolicyEditProposalChangeType = "update_documentation_source"
	PolicyEditProposalChangeUpdatePolicy        PolicyEditProposalChangeType = "update_policy"
)
