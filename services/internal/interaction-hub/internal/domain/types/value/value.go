package value

import (
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/enum"
)

type ScopeRef struct {
	Type enum.ScopeType
	Ref  string
}

type Actor struct {
	Type string
	ID   string
}

func (a Actor) Ref() string {
	if a.Type == "" || a.ID == "" {
		return ""
	}
	return a.Type + ":" + a.ID
}

type CommandMeta struct {
	CommandID       uuid.UUID
	IdempotencyKey  string
	ExpectedVersion *int64
	Actor           Actor
	Reason          string
	RequestID       string
}

type QueryMeta struct {
	Actor     Actor
	RequestID string
}

type ObjectRef struct {
	URI       string
	Digest    string
	SizeBytes *int64
}

type ActorRef struct {
	Kind string
	Ref  string
}

func (r ActorRef) String() string {
	if r.Kind == "" || r.Ref == "" {
		return ""
	}
	return r.Kind + ":" + r.Ref
}

type ExternalRef struct {
	Kind string
	Ref  string
}

type SourceOwnerRef struct {
	Kind enum.SourceOwnerKind
	Ref  string
}

type IngressRef struct {
	Kind enum.IngressKind
	Ref  string
}

type DecisionOwnerRef struct {
	Kind             enum.DecisionOwnerKind
	OwnerRequestRef  string
	OwnerDecisionRef string
}

type InteractionAction struct {
	ActionKey        string
	LabelTemplateRef string
	Terminal         bool
}

type PageRequest struct {
	PageSize  int32
	PageToken string
}

type PageResult struct {
	NextPageToken string
}

type Clock interface {
	Now() time.Time
}

type UUIDGenerator interface {
	New() uuid.UUID
}
