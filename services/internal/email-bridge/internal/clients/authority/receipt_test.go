package authority

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/repository/receipt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type effectRuntime struct {
	cp.RuntimeWorkServiceClient
	report    func(context.Context, *cp.ReportEmailEffectReceiptRequest) (*cp.ReportEmailEffectReceiptResponse, error)
	reconcile func(context.Context, *cp.ResolveEmailReconciliationRequest) (*cp.ResolveEmailReconciliationResponse, error)
}

func (f effectRuntime) ReportEmailEffectReceipt(ctx context.Context, r *cp.ReportEmailEffectReceiptRequest, _ ...grpc.CallOption) (*cp.ReportEmailEffectReceiptResponse, error) {
	return f.report(ctx, r)
}

func (f effectRuntime) ResolveEmailReconciliation(ctx context.Context, r *cp.ResolveEmailReconciliationRequest, _ ...grpc.CallOption) (*cp.ResolveEmailReconciliationResponse, error) {
	return f.reconcile(ctx, r)
}

func effectFixture() (receipt.Report, *cp.EmailEffectReceipt) {
	r := receipt.OwnerReceipt{Invocation: "inv_fixture01", ExternalRef: strings.Repeat("a", 32), ExternalDigest: strings.Repeat("b", 64), InputDigest: strings.Repeat("c", 64), EffectKey: "effect:with/slashes", Mailbox: "mailbox01", Connection: "connection01", ConfigurationRevision: 7, Outcome: receipt.Unknown}
	return receipt.Report{Binding: fixtureRequest().ExecutionBinding, Receipt: r, IdempotencyKey: "report_fixture01"}, &cp.EmailEffectReceipt{Ref: "receipt_fixture01", Version: 1, InvocationRef: r.Invocation, ExternalReceiptRef: r.ExternalRef, ExternalReceiptDigest: r.ExternalDigest, SemanticInputDigest: r.InputDigest, EffectKey: r.EffectKey, MailboxRef: r.Mailbox, ConnectionRef: r.Connection, ConfigurationRevision: r.ConfigurationRevision, Outcome: cp.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_UNKNOWN_OUTCOME}
}

func TestReportEmailReceiptExactReadback(t *testing.T) {
	for _, field := range []string{"valid", "nil", "ref", "version", "invocation", "external-ref", "digest", "input", "key", "mailbox", "connection", "configuration", "outcome", "unknown-enum"} {
		t.Run(field, func(t *testing.T) {
			input, response := effectFixture()
			calls := 0
			f := effectRuntime{report: func(ctx context.Context, r *cp.ReportEmailEffectReceiptRequest) (*cp.ReportEmailEffectReceiptResponse, error) {
				calls++
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > effectAuthorityTimeout || !proto.Equal(r.Binding, Binding(input.Binding)) || r.Mutation.ExpectedVersion != nil || r.Mutation.IdempotencyKey != input.IdempotencyKey || r.SemanticInputDigest != input.Receipt.InputDigest || r.ExternalReceiptDigest != input.Receipt.ExternalDigest || r.ExternalReceiptRef != input.Receipt.ExternalRef || r.Outcome != response.Outcome {
					t.Fatal("report mapping or deadline mismatch")
				}
				switch field {
				case "nil":
					return nil, nil
				case "ref":
					response.Ref = "bad"
				case "version":
					response.Version = 0
				case "invocation":
					response.InvocationRef = "other"
				case "external-ref":
					response.ExternalReceiptRef = strings.Repeat("d", 32)
				case "digest":
					response.ExternalReceiptDigest = strings.Repeat("d", 64)
				case "input":
					response.SemanticInputDigest = strings.Repeat("d", 64)
				case "key":
					response.EffectKey = "other"
				case "mailbox":
					response.MailboxRef = "other"
				case "connection":
					response.ConnectionRef = "other"
				case "configuration":
					response.ConfigurationRevision++
				case "outcome":
					response.Outcome = cp.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_EFFECT_CONFIRMED
				case "unknown-enum":
					response.Outcome = 777
				}
				return &cp.ReportEmailEffectReceiptResponse{Receipt: response}, nil
			}}
			got, err := (&Client{API: f}).Report(t.Context(), input)
			if calls != 1 || (field == "valid" && (err != nil || got.Ref != response.Ref)) || (field != "valid" && !errors.Is(err, errs.Unavailable)) {
				t.Fatalf("readback result calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestReportEmailReceiptInputAndDeadline(t *testing.T) {
	for _, name := range []string{"nil-binding", "expired", "test-source", "invocation", "prefixed-digest", "uppercase", "short-ref", "outcome", "idempotency"} {
		t.Run(name, func(t *testing.T) {
			input, _ := effectFixture()
			switch name {
			case "nil-binding":
				input.Binding = nil
			case "expired":
				input.Binding.Lease.ExpiresAt = time.Now().Add(-time.Second)
			case "test-source":
				input.Binding.ConnectionTestRef = input.Binding.InvocationRef
				input.Binding.InvocationRef = nil
			case "invocation":
				input.Receipt.Invocation = "other"
			case "prefixed-digest":
				input.Receipt.ExternalDigest = "sha256:" + input.Receipt.ExternalDigest
			case "uppercase":
				input.Receipt.ExternalDigest = strings.ToUpper(input.Receipt.ExternalDigest)
			case "short-ref":
				input.Receipt.ExternalRef = "bad"
			case "outcome":
				input.Receipt.Outcome = "RETRY"
			case "idempotency":
				input.IdempotencyKey = ""
			}
			f := effectRuntime{report: func(context.Context, *cp.ReportEmailEffectReceiptRequest) (*cp.ReportEmailEffectReceiptResponse, error) {
				t.Fatal("invalid request reached CP")
				return nil, nil
			}}
			if _, err := (&Client{API: f}).Report(t.Context(), input); !errors.Is(err, errs.Invalid) {
				t.Fatalf("got %v", err)
			}
		})
	}
	for _, code := range []codes.Code{codes.PermissionDenied, codes.Unauthenticated, codes.Unimplemented, codes.Unavailable} {
		t.Run(code.String(), func(t *testing.T) {
			input, _ := effectFixture()
			calls := 0
			f := effectRuntime{report: func(context.Context, *cp.ReportEmailEffectReceiptRequest) (*cp.ReportEmailEffectReceiptResponse, error) {
				calls++
				return nil, status.Error(code, "fixture")
			}}
			_, err := (&Client{API: f}).Report(t.Context(), input)
			want := errs.Unavailable
			if code == codes.PermissionDenied || code == codes.Unauthenticated {
				want = errs.Denied
			}
			if !errors.Is(err, want) || calls != 1 {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
		})
	}
	input, _ := effectFixture()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	f := effectRuntime{report: func(ctx context.Context, _ *cp.ReportEmailEffectReceiptRequest) (*cp.ReportEmailEffectReceiptResponse, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	start := time.Now()
	if _, err := (&Client{API: f}).Report(ctx, input); !errors.Is(err, errs.Unavailable) || time.Since(start) > time.Second {
		t.Fatal("report deadline was not preserved")
	}
}

func TestReconcileEmailReceiptExactDecision(t *testing.T) {
	for _, field := range []string{"valid", "no-effect", "nil", "receipt", "digest", "version", "decision", "decision-version", "decision-receipt", "decision-digest", "decision-receipt-version", "invocation", "grant", "actor", "expired", "created", "unknown", "unknown-enum"} {
		t.Run(field, func(t *testing.T) {
			input, r := effectFixture()
			input.Receipt.Ref, input.Receipt.Version = r.Ref, r.Version
			d := &cp.EmailReconciliationDecision{Ref: "decision_fixture01", Version: 1, ReceiptRef: r.Ref, ReceiptVersion: r.Version, ReceiptDigest: r.ExternalReceiptDigest, InvocationRef: r.InvocationRef, GrantRef: "grant_fixture01", ActorRef: "actor_fixture01", Outcome: cp.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_EFFECT_CONFIRMED, CreatedAt: timestamppb.New(time.Now().Add(-time.Second)), ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}
			calls := 0
			f := effectRuntime{reconcile: func(ctx context.Context, request *cp.ResolveEmailReconciliationRequest) (*cp.ResolveEmailReconciliationResponse, error) {
				calls++
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > effectAuthorityTimeout || request.ReceiptRef != r.Ref || request.DecisionRef != d.Ref || request.ExternalReceiptRef != r.ExternalReceiptRef || request.ExternalReceiptDigest != r.ExternalReceiptDigest {
					t.Fatal("reconciliation mapping mismatch")
				}
				switch field {
				case "no-effect":
					d.Outcome = cp.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_NO_EFFECT_CONFIRMED
				case "nil":
					return nil, nil
				case "receipt":
					r.Ref = "receipt_other01"
				case "digest":
					r.ExternalReceiptDigest = strings.Repeat("d", 64)
				case "version":
					r.Version++
				case "decision":
					d.Ref = "decision_other01"
				case "decision-version":
					d.Version = 0
				case "decision-receipt":
					d.ReceiptRef = "receipt_other01"
				case "decision-digest":
					d.ReceiptDigest = strings.Repeat("d", 64)
				case "decision-receipt-version":
					d.ReceiptVersion++
				case "invocation":
					d.InvocationRef = "other"
				case "grant":
					d.GrantRef = ""
				case "actor":
					d.ActorRef = ""
				case "expired":
					d.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
				case "created":
					d.CreatedAt = timestamppb.New(time.Now().Add(time.Minute))
				case "unknown":
					d.Outcome = cp.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_UNKNOWN_OUTCOME
				case "unknown-enum":
					d.Outcome = 777
				}
				return &cp.ResolveEmailReconciliationResponse{Receipt: r, Decision: d}, nil
			}}
			got, err := (&Client{API: f}).Reconcile(t.Context(), input.Receipt, d.Ref)
			valid := field == "valid" || field == "no-effect"
			if calls != 1 || valid && (err != nil || got.Ref == "") || !valid && !errors.Is(err, errs.Unavailable) {
				t.Fatalf("decision calls=%d err=%v", calls, err)
			}
		})
	}
}
