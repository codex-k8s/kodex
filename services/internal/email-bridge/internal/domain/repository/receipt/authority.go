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
