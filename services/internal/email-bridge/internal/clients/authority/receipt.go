package authority

import (
	"context"
	"regexp"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/repository/receipt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const effectAuthorityTimeout = 3 * time.Second

var (
	receiptRefPattern  = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)
	externalRefPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	digestPattern      = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

var _ receipt.EffectAuthority = (*Client)(nil)

func (c *Client) Report(ctx context.Context, input receipt.Report) (receipt.OwnerReceipt, error) {
	want := input.Receipt
	binding := Binding(input.Binding)
	outcome, ok := effectOutcome(want.Outcome)
	if c.API == nil || binding == nil || binding.Lease.ExpiresAt.CheckValid() != nil || binding.GetInvocationRef() != want.Invocation || want.Invocation == "" || !binding.Lease.ExpiresAt.AsTime().After(time.Now()) || !validReceipt(want, false) || !ok || !receiptRefPattern.MatchString(input.IdempotencyKey) {
		return receipt.OwnerReceipt{}, errs.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, effectAuthorityTimeout)
	defer cancel()
	ctx, cancelLease := context.WithDeadline(ctx, binding.Lease.ExpiresAt.AsTime())
	defer cancelLease()
	mutation := &cp.MutationContext{IdempotencyKey: input.IdempotencyKey}
	response, err := c.API.ReportEmailEffectReceipt(ctx, &cp.ReportEmailEffectReceiptRequest{Mutation: mutation, Binding: binding, ExternalReceiptRef: want.ExternalRef, ExternalReceiptDigest: want.ExternalDigest, SemanticInputDigest: want.InputDigest, Outcome: outcome})
	if err != nil {
		return receipt.OwnerReceipt{}, effectError(err)
	}
	got, ok := ownerReceipt(response.GetReceipt())
	if !ok || !sameReceipt(got, want) || got.Outcome != want.Outcome || (want.Ref != "" && got.Ref != want.Ref) || got.Version < want.Version {
		return receipt.OwnerReceipt{}, errs.Unavailable
	}
	return got, nil
}

func (c *Client) Reconcile(ctx context.Context, want receipt.OwnerReceipt, decisionRef string) (receipt.Decision, error) {
	if c.API == nil || !validReceipt(want, true) || want.Outcome != receipt.Unknown || (decisionRef != "" && !receiptRefPattern.MatchString(decisionRef)) {
		return receipt.Decision{}, errs.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, effectAuthorityTimeout)
	defer cancel()
	response, err := c.API.ResolveEmailReconciliation(ctx, &cp.ResolveEmailReconciliationRequest{ReceiptRef: want.Ref, DecisionRef: decisionRef, ExternalReceiptRef: want.ExternalRef, ExternalReceiptDigest: want.ExternalDigest})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return receipt.Decision{}, errs.NotFound
		}
		return receipt.Decision{}, effectError(err)
	}
	got, ok := ownerReceipt(response.GetReceipt())
	d := response.GetDecision()
	if !ok || !sameReceipt(got, want) || got.Ref != want.Ref || got.Version != want.Version || got.Outcome != receipt.Unknown || d == nil || !receiptRefPattern.MatchString(d.Ref) || (decisionRef != "" && d.Ref != decisionRef) || d.Version < 1 || d.ReceiptRef != got.Ref || d.ReceiptVersion != got.Version || d.ReceiptDigest != got.ExternalDigest || d.InvocationRef != got.Invocation || d.ActorRef == "" || d.GrantRef == "" || d.ExpiresAt == nil || d.ExpiresAt.CheckValid() != nil || !d.ExpiresAt.AsTime().After(time.Now()) || d.CreatedAt == nil || d.CreatedAt.CheckValid() != nil || d.CreatedAt.AsTime().After(time.Now()) || !d.ExpiresAt.AsTime().After(d.CreatedAt.AsTime()) {
		return receipt.Decision{}, errs.Unavailable
	}
	if d.Outcome != cp.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_EFFECT_CONFIRMED && d.Outcome != cp.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_NO_EFFECT_CONFIRMED {
		return receipt.Decision{}, errs.Unavailable
	}
	outcome := receipt.EffectConfirmed
	if d.Outcome == cp.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_NO_EFFECT_CONFIRMED {
		outcome = receipt.NoEffectConfirmed
	}
	return receipt.Decision{Ref: d.Ref, Version: d.Version, Actor: d.ActorRef, Grant: d.GrantRef, Receipt: got, Outcome: outcome, ExpiresAt: d.ExpiresAt.AsTime()}, nil
}

func effectOutcome(value receipt.Outcome) (cp.EmailEffectOutcome, bool) {
	switch value {
	case receipt.Unknown:
		return cp.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_UNKNOWN_OUTCOME, true
	case receipt.EffectConfirmed:
		return cp.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_EFFECT_CONFIRMED, true
	case receipt.NoEffectConfirmed:
		return cp.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_NO_EFFECT_CONFIRMED, true
	default:
		return 0, false
	}
}

func ownerReceipt(r *cp.EmailEffectReceipt) (receipt.OwnerReceipt, bool) {
	if r == nil {
		return receipt.OwnerReceipt{}, false
	}
	outcome := receipt.Unknown
	switch r.Outcome {
	case cp.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_UNKNOWN_OUTCOME:
	case cp.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_EFFECT_CONFIRMED:
		outcome = receipt.EffectConfirmed
	case cp.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_NO_EFFECT_CONFIRMED:
		outcome = receipt.NoEffectConfirmed
	default:
		return receipt.OwnerReceipt{}, false
	}
	value := receipt.OwnerReceipt{Ref: r.Ref, Version: r.Version, Invocation: r.InvocationRef, ExternalRef: r.ExternalReceiptRef, ExternalDigest: r.ExternalReceiptDigest, InputDigest: r.SemanticInputDigest, EffectKey: r.EffectKey, Mailbox: r.MailboxRef, Connection: r.ConnectionRef, ConfigurationRevision: r.ConfigurationRevision, Outcome: outcome}
	return value, validReceipt(value, true)
}

func validReceipt(r receipt.OwnerReceipt, owner bool) bool {
	return (!owner || receiptRefPattern.MatchString(r.Ref) && r.Version > 0) && externalRefPattern.MatchString(r.ExternalRef) && digestPattern.MatchString(r.ExternalDigest) && digestPattern.MatchString(r.InputDigest) && r.Invocation != "" && r.EffectKey != "" && len(r.EffectKey) <= 128 && r.Mailbox != "" && r.Connection != "" && r.ConfigurationRevision > 0
}

func sameReceipt(a, b receipt.OwnerReceipt) bool {
	return a.Invocation == b.Invocation && a.ExternalRef == b.ExternalRef && a.ExternalDigest == b.ExternalDigest && a.InputDigest == b.InputDigest && a.EffectKey == b.EffectKey && a.Mailbox == b.Mailbox && a.Connection == b.Connection && a.ConfigurationRevision == b.ConfigurationRevision
}

func effectError(err error) error {
	if status.Code(err) == codes.PermissionDenied || status.Code(err) == codes.Unauthenticated {
		return errs.Denied
	}
	return errs.Unavailable
}
