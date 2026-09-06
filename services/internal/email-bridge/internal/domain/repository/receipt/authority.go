package receipt

import (
	"context"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
)

type Outcome string

const (
	Unknown           Outcome = "UNKNOWN_OUTCOME"
	EffectConfirmed   Outcome = "EFFECT_CONFIRMED"
	NoEffectConfirmed Outcome = "NO_EFFECT_CONFIRMED"
)

type OwnerReceipt struct {
	Ref                                                             string
	Version                                                         int64
	Invocation, ExternalRef, ExternalDigest, InputDigest, EffectKey string
	Mailbox, Connection                                             string
	ConfigurationRevision                                           int64
	Outcome                                                         Outcome
}

type Report struct {
	Binding        *api.ExecutionBinding
	Receipt        OwnerReceipt
	IdempotencyKey string
	Replay         bool
}

type Decision struct {
	Ref, Actor, Grant string
	Version           int64
	Receipt           OwnerReceipt
	Outcome           Outcome
	ExpiresAt         time.Time
}

type EffectAuthority interface {
	Report(context.Context, Report) (OwnerReceipt, error)
	Reconcile(context.Context, OwnerReceipt, string) (Decision, error)
}

type Pending struct {
	Scope  Scope
	Record Record
	Owner  OwnerReceipt
}

type ReconciliationRepository interface {
	Remember(context.Context, Scope, Record, OwnerReceipt, time.Time) error
	Pending(context.Context, int) ([]Pending, error)
	ClaimReconciliation(context.Context, Pending, time.Duration) (bool, error)
	CommitReconciliation(context.Context, Pending, Decision) (bool, error)
}

func (a OwnerReceipt) Same(b OwnerReceipt) bool {
	return a == b
}

func (p Pending) Valid() bool {
	r, o := p.Record, p.Owner
	return p.Scope.Tenant != "" && p.Scope.Mailbox != "" && r.Status == "unknown" && r.Audit.Valid() && o.Ref != "" && o.Version > 0 && o.Invocation != "" && o.Connection != "" && o.Mailbox == p.Scope.Mailbox && o.ExternalRef == r.ID && o.ExternalDigest == r.ExternalDigest(p.Scope) && o.InputDigest == r.Digest && o.EffectKey == r.Key && o.ConfigurationRevision == r.Audit.ConfigurationRevision && o.Outcome == Unknown
}

func (d Decision) ValidFor(p Pending, now time.Time) bool {
	return p.Valid() && d.Receipt.Same(p.Owner) && d.Ref != "" && d.Version > 0 && d.Actor != "" && d.Grant != "" && (d.Outcome == EffectConfirmed || d.Outcome == NoEffectConfirmed) && d.ExpiresAt.After(now.Add(100*time.Millisecond))
}
