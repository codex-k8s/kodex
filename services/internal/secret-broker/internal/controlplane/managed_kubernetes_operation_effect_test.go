package controlplane

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// effectClientFixture controls only protected RPC responses and counts uncertain admission attempts.
// effectClientFixture управляет только protected RPC responses и считает uncertain admission attempts.
type effectClientFixture struct {
	frameClientFixture
	beginEffect                           *cp.BeginManagedKubernetesEffectResponse
	completeEffect                        *cp.CompleteManagedKubernetesEffectResponse
	beginEffectRequest                    *cp.BeginManagedKubernetesEffectRequest
	completeEffectRequest                 *cp.CompleteManagedKubernetesEffectRequest
	beginEffectCalls, completeEffectCalls int
}

// BeginManagedKubernetesEffect records one original binding without simulating owner persistence.
// BeginManagedKubernetesEffect фиксирует original binding без имитации owner persistence.
func (client *effectClientFixture) BeginManagedKubernetesEffect(_ context.Context, request *cp.BeginManagedKubernetesEffectRequest, _ ...grpc.CallOption) (*cp.BeginManagedKubernetesEffectResponse, error) {
	client.beginEffectCalls++
	client.beginEffectRequest = request
	return client.beginEffect, client.err
}

// CompleteManagedKubernetesEffect captures exact safe bytes and returns independently selected state.
// CompleteManagedKubernetesEffect фиксирует exact safe bytes и возвращает независимо выбранное state.
func (client *effectClientFixture) CompleteManagedKubernetesEffect(_ context.Context, request *cp.CompleteManagedKubernetesEffectRequest, _ ...grpc.CallOption) (*cp.CompleteManagedKubernetesEffectResponse, error) {
	client.completeEffectCalls++
	client.completeEffectRequest = request
	return client.completeEffect, client.err
}

// operationEffectFixture supplies synthetic owner pins and bounded future admission.
// operationEffectFixture задаёт synthetic owner pins и ограниченный future admission.
func operationEffectFixture() (*Owner, *effectClientFixture, managedkubernetes.Binding, targetrepo.EffectOpening, targetrepo.EffectCompletion) {
	owner, base, binding, _ := frameAdapterFixture()
	opening := targetrepo.EffectOpening{PlanRef: "plan_synthetic", PlanRevision: 2, PlanDigest: strings.Repeat("a", 64), ChildRef: "child_synthetic", ChildInputDigest: strings.Repeat("b", 64), Operation: "kubernetes.get", HardStreamDeadline: time.Now().Add(5 * time.Minute), ExpiresAt: time.Now().Add(time.Minute)}
	completion := targetrepo.EffectCompletion{EffectRef: "effect_synthetic", EffectGeneration: 3, ChildInputDigest: opening.ChildInputDigest, Outcome: "SUCCEEDED", ResultJSON: []byte(`{"metadata":{"name":"synthetic"}}`)}
	digest := sha256.Sum256(completion.ResultJSON)
	client := &effectClientFixture{frameClientFixture: *base, beginEffect: &cp.BeginManagedKubernetesEffectResponse{EffectRef: completion.EffectRef, EffectGeneration: completion.EffectGeneration, ChildInputDigest: opening.ChildInputDigest, ExpiresAt: timestamppb.New(opening.ExpiresAt)}, completeEffect: &cp.CompleteManagedKubernetesEffectResponse{State: "SUCCEEDED", ResultDigest: hex.EncodeToString(digest[:]), SubmissionDigest: hex.EncodeToString(digest[:]), Applied: true, AuthorizedUntil: timestamppb.New(time.Now().Add(20 * time.Second))}}
	owner.client.KubernetesOperations = client
	return owner, client, binding, opening, completion
}

// TestOperationEffectPreservesPinsAndTerminalOverride verifies exact lineage and a concurrent cancellation.
// TestOperationEffectPreservesPinsAndTerminalOverride проверяет exact lineage и concurrent cancellation.
func TestOperationEffectPreservesPinsAndTerminalOverride(t *testing.T) {
	owner, client, binding, opening, completion := operationEffectFixture()
	admission, err := owner.BeginKubernetesEffect(t.Context(), binding, opening)
	if err != nil || admission.EffectRef != completion.EffectRef || admission.ReconcileOnly || admission.GateRef != "" || admission.Stream != nil || client.beginEffectCalls != 1 {
		t.Fatal("fresh admission failed")
	}
	r := client.beginEffectRequest
	if !proto.Equal(r.Binding, kubernetesHealthBindingToProto(binding)) || r.PlanRef != opening.PlanRef || r.PlanRevision != opening.PlanRevision || r.PlanDigest != opening.PlanDigest || r.ChildRef != opening.ChildRef || r.ChildInputDigest != opening.ChildInputDigest {
		t.Fatal("child lineage changed")
	}
	client.completeEffect.State = "CANCELLED"
	client.completeEffect.Applied = false
	client.completeEffect.AuthorizedUntil = nil
	client.completeEffect.ResultDigest = kubernetesDiscardedResultDigest
	receipt, err := owner.CompleteKubernetesEffect(t.Context(), binding, completion)
	if err != nil || receipt.State != "CANCELLED" || client.completeEffectCalls != 1 || !proto.Equal(client.completeEffectRequest.Binding, r.Binding) || client.completeEffectRequest.EffectRef != completion.EffectRef || client.completeEffectRequest.ChildInputDigest != opening.ChildInputDigest {
		t.Fatal("terminal override or completion pins changed")
	}
	completion.ResultJSON[0] = '!'
	if client.completeEffectRequest.ResultJson[0] != '{' {
		t.Fatal("completion retained mutable source bytes")
	}
}

// TestOperationEffectAdmissionRejectsChangedCredit covers malformed and widened owner replies without retry.
// TestOperationEffectAdmissionRejectsChangedCredit проверяет malformed и widened owner replies без retry.
func TestOperationEffectAdmissionRejectsChangedCredit(t *testing.T) {
	for _, scenario := range []string{"nil", "lost", "unknown", "effect", "generation", "input", "missing-time", "invalid-time", "time-unknown", "expired", "extended", "missing-stream", "unexpected-stream", "unhandled-history"} {
		t.Run(scenario, func(t *testing.T) {
			owner, client, binding, opening, _ := operationEffectFixture()
			switch scenario {
			case "nil":
				client.beginEffect = nil
			case "lost":
				client.err = errors.New("synthetic lost response")
			case "unknown":
				client.beginEffect.ProtoReflect().SetUnknown([]byte{0x78, 1})
			case "effect":
				client.beginEffect.EffectRef = ""
			case "generation":
				client.beginEffect.EffectGeneration = 0
			case "input":
				client.beginEffect.ChildInputDigest = strings.Repeat("c", 64)
			case "missing-time":
				client.beginEffect.ExpiresAt = nil
			case "invalid-time":
				client.beginEffect.ExpiresAt = &timestamppb.Timestamp{Seconds: 253402300800}
			case "time-unknown":
				client.beginEffect.ExpiresAt.ProtoReflect().SetUnknown([]byte{0x78, 1})
			case "expired":
				client.beginEffect.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
			case "extended":
				client.beginEffect.ExpiresAt = timestamppb.New(opening.ExpiresAt.Add(time.Second))
			case "missing-stream":
				opening.Operation = "kubernetes.exec"
			case "unexpected-stream":
				client.beginEffect.Stream = &cp.ManagedKubernetesOwnerStreamGrant{}
			case "unhandled-history":
				client.beginEffect.ReconcileOnly = true
				client.beginEffect.StreamHistory = &cp.ManagedKubernetesStreamHistory{}
			}
			admission, err := owner.BeginKubernetesEffect(t.Context(), binding, opening)
			if err == nil || admission.EffectRef != "" || admission.GateRef != "" || admission.Stream != nil || client.beginEffectCalls != 1 {
				t.Fatal("invalid response supplied authority or retried")
			}
		})
	}
}

// TestOperationEffectGateRequiresExactOwnerScope prevents generic preconditions from fabricating approvals.
// TestOperationEffectGateRequiresExactOwnerScope не превращает generic preconditions в approvals.
func TestOperationEffectGateRequiresExactOwnerScope(t *testing.T) {
	for _, scenario := range []string{"valid", "domain", "reason", "plan", "child", "gate", "extra", "unknown", "code", "duplicate"} {
		t.Run(scenario, func(t *testing.T) {
			owner, client, binding, opening, _ := operationEffectFixture()
			info := &errdetails.ErrorInfo{Domain: "kodex.control-plane", Reason: "KUBERNETES_APPROVAL_REQUIRED", Metadata: map[string]string{"gateRef": "gate_synthetic", "planRef": opening.PlanRef, "childRef": opening.ChildRef}}
			code := codes.FailedPrecondition
			switch scenario {
			case "domain":
				info.Domain = "other"
			case "reason":
				info.Reason = "OTHER"
			case "plan":
				info.Metadata["planRef"] = "plan_foreign"
			case "child":
				info.Metadata["childRef"] = "child_foreign"
			case "gate":
				info.Metadata["gateRef"] = ""
			case "extra":
				info.Metadata["effectRef"] = "effect_synthetic"
			case "unknown":
				info.ProtoReflect().SetUnknown([]byte{0x78, 1})
			case "code":
				code = codes.PermissionDenied
			}
			s, err := status.New(code, "synthetic private diagnostic").WithDetails(info)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "duplicate" {
				s, err = s.WithDetails(info)
				if err != nil {
					t.Fatal(err)
				}
			}
			client.err = s.Err()
			admission, err := owner.BeginKubernetesEffect(t.Context(), binding, opening)
			if admission.EffectRef != "" || admission.Stream != nil || client.beginEffectCalls != 1 {
				t.Fatal("gate issued effect authority")
			}
			if scenario == "valid" {
				if err != nil || admission.GateRef != "gate_synthetic" {
					t.Fatal("valid gate missing")
				}
			} else if err == nil || admission.GateRef != "" {
				t.Fatal("foreign gate accepted")
			}
		})
	}
}

// TestOperationEffectCompletionRejectsUnsafeSyntaxAndReceipts keeps malformed bytes away from RPC and rejects false acknowledgments.
// TestOperationEffectCompletionRejectsUnsafeSyntaxAndReceipts не отправляет malformed bytes и отклоняет false acknowledgments.
func TestOperationEffectCompletionRejectsUnsafeSyntaxAndReceipts(t *testing.T) {
	for _, raw := range []string{"", "null", "[]", `{"a":1,"a":2}`, `{"nested":{"a":1,"a":2}}`, `{} {}`, string([]byte{'{', '"', 'a', '"', ':', '"', 255, '"', '}'}), strings.Repeat(" ", 64<<10) + "{}"} {
		owner, client, binding, _, completion := operationEffectFixture()
		completion.ResultJSON = []byte(raw)
		if _, err := owner.CompleteKubernetesEffect(t.Context(), binding, completion); err == nil || client.completeEffectCalls != 0 {
			t.Fatal("invalid result reached owner RPC")
		}
	}
	for _, scenario := range []string{"nil", "lost", "unknown", "digest", "state"} {
		t.Run(scenario, func(t *testing.T) {
			owner, client, binding, _, completion := operationEffectFixture()
			switch scenario {
			case "nil":
				client.completeEffect = nil
			case "lost":
				client.err = errors.New("synthetic lost ACK")
			case "unknown":
				client.completeEffect.ProtoReflect().SetUnknown([]byte{0x78, 1})
			case "digest":
				client.completeEffect.ResultDigest = strings.Repeat("e", 64)
			case "state":
				client.completeEffect.State = "ATTEMPTING"
			}
			if receipt, err := owner.CompleteKubernetesEffect(t.Context(), binding, completion); err == nil || receipt.State != "" || client.completeEffectCalls != 1 {
				t.Fatal("uncertain ACK accepted or retried")
			}
		})
	}
}

// TestOperationEffectReconcileAndCancelledCallerSupplyNoDispatch proves expired evidence and caller cancellation remain non-executable.
// TestOperationEffectReconcileAndCancelledCallerSupplyNoDispatch проверяет отсутствие dispatch для expired evidence и caller cancellation.
func TestOperationEffectReconcileAndCancelledCallerSupplyNoDispatch(t *testing.T) {
	owner, client, binding, opening, _ := operationEffectFixture()
	client.beginEffect.ReconcileOnly = true
	client.beginEffect.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
	admission, err := owner.BeginKubernetesEffect(t.Context(), binding, opening)
	if err != nil || !admission.ReconcileOnly || admission.Stream != nil {
		t.Fatal("reconciliation acquired fresh transport credit")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = owner.BeginKubernetesEffect(ctx, binding, opening); err == nil || client.beginEffectCalls != 1 {
		t.Fatal("cancelled caller reached RPC")
	}
}

// TestOperationEffectStreamCreditAndHistoricalReadback separates fresh grants from expired immutable evidence.
// TestOperationEffectStreamCreditAndHistoricalReadback отделяет fresh grants от expired immutable evidence.
func TestOperationEffectStreamCreditAndHistoricalReadback(t *testing.T) {
	for _, scenario := range []string{"fresh", "long-stream", "historical", "generation", "expiry", "channels", "nested-unknown"} {
		t.Run(scenario, func(t *testing.T) {
			owner, client, binding, opening, _ := operationEffectFixture()
			opening.Operation = "kubernetes.exec"
			stream := &cp.ManagedKubernetesOwnerStreamGrant{StreamRef: "stream_synthetic", Generation: binding.Generation, ExpiresAt: timestamppb.New(opening.ExpiresAt), MaximumFrameBytes: 1024, MaximumTotalBytes: 4096, InputChannels: []string{"STDIN"}, OutputChannels: []string{"STDOUT"}}
			client.beginEffect.Stream = stream
			switch scenario {
			case "historical":
				client.beginEffect.ReconcileOnly = true
				client.beginEffect.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
				stream.ExpiresAt = client.beginEffect.ExpiresAt
			case "generation":
				stream.Generation++
			case "long-stream":
				stream.ExpiresAt = timestamppb.New(opening.HardStreamDeadline)
			case "expiry":
				stream.ExpiresAt = timestamppb.New(opening.HardStreamDeadline.Add(time.Second))
			case "channels":
				stream.InputChannels = []string{"STDIN", "STDIN"}
			case "nested-unknown":
				stream.ProtoReflect().SetUnknown([]byte{0x78, 1})
			}
			admission, err := owner.BeginKubernetesEffect(t.Context(), binding, opening)
			switch scenario {
			case "fresh", "long-stream":
				if err != nil || admission.Stream == nil || admission.ReconcileOnly {
					t.Fatal("fresh stream grant missing")
				}
				stream.OutputChannels[0] = "STDERR"
				if admission.Stream.OutputChannels[0] != "STDOUT" {
					t.Fatal("mutable wire channels changed admitted stream")
				}
			case "historical":
				if err != nil || !admission.ReconcileOnly || admission.Stream != nil {
					t.Fatal("historical stream lost readback or acquired executable credit")
				}
			default:
				if err == nil || admission.Stream != nil || admission.EffectRef != "" {
					t.Fatal("malformed stream supplied authority")
				}
			}
		})
	}
}

// TestOperationEffectReceiptCannotChangeObservedOutcome binds semantic outcome in addition to exact JSON bytes.
// TestOperationEffectReceiptCannotChangeObservedOutcome связывает semantic outcome помимо exact JSON bytes.
func TestOperationEffectReceiptCannotChangeObservedOutcome(t *testing.T) {
	for _, observed := range []string{"SUCCEEDED", "FAILED", "UNKNOWN"} {
		for _, state := range []string{"SUCCEEDED", "FAILED", "UNKNOWN", "CANCELLED"} {
			t.Run(observed+"/"+state, func(t *testing.T) {
				owner, client, binding, _, completion := operationEffectFixture()
				completion.Outcome = observed
				if observed != "SUCCEEDED" {
					completion.ReasonCode = "KUBERNETES_TRANSPORT_UNKNOWN"
				}
				client.completeEffect.State = state
				if state == "CANCELLED" {
					client.completeEffect.Applied = false
					client.completeEffect.AuthorizedUntil = nil
					client.completeEffect.ResultDigest = kubernetesDiscardedResultDigest
				}
				receipt, err := owner.CompleteKubernetesEffect(t.Context(), binding, completion)
				allowed := state == observed || state == "CANCELLED"
				if allowed && (err != nil || receipt.State != state) || !allowed && (err == nil || receipt.State != "") || client.completeEffectCalls != 1 {
					t.Fatal("receipt changed observed outcome or lost cancellation override")
				}
			})
		}
	}
}

// TestOperationEffectOutputCreditRejectsAmbiguousReceipts distinguishes accepted bytes from late discarded reads.
// TestOperationEffectOutputCreditRejectsAmbiguousReceipts отделяет accepted bytes от late discarded reads.
func TestOperationEffectOutputCreditRejectsAmbiguousReceipts(t *testing.T) {
	for _, scenario := range []string{"submission", "missing-credit", "expired-credit", "wide-credit", "unknown-credit", "malformed-credit", "discarded-success", "discarded-payload", "discarded-credit", "applied-cancel"} {
		t.Run(scenario, func(t *testing.T) {
			owner, client, binding, _, completion := operationEffectFixture()
			switch scenario {
			case "submission":
				client.completeEffect.SubmissionDigest = strings.Repeat("d", 64)
			case "missing-credit":
				client.completeEffect.AuthorizedUntil = nil
			case "expired-credit":
				client.completeEffect.AuthorizedUntil = timestamppb.New(time.Now().Add(-time.Second))
			case "wide-credit":
				client.completeEffect.AuthorizedUntil = timestamppb.New(time.Now().Add(time.Minute))
			case "unknown-credit":
				client.completeEffect.AuthorizedUntil.ProtoReflect().SetUnknown([]byte{0x78, 1})
			case "malformed-credit":
				client.completeEffect.AuthorizedUntil = &timestamppb.Timestamp{Seconds: 253402300800}
			case "discarded-success":
				client.completeEffect.Applied = false
				client.completeEffect.AuthorizedUntil = nil
				client.completeEffect.ResultDigest = kubernetesDiscardedResultDigest
			case "discarded-payload":
				client.completeEffect.Applied = false
				client.completeEffect.State = "CANCELLED"
				client.completeEffect.AuthorizedUntil = nil
			case "discarded-credit":
				client.completeEffect.Applied = false
				client.completeEffect.State = "CANCELLED"
				client.completeEffect.ResultDigest = kubernetesDiscardedResultDigest
			case "applied-cancel":
				client.completeEffect.State = "CANCELLED"
			}
			receipt, err := owner.CompleteKubernetesEffect(t.Context(), binding, completion)
			if err == nil || receipt.State != "" || !receipt.AuthorizedUntil.IsZero() || receipt.Applied || client.completeEffectCalls != 1 {
				t.Fatal("invalid receipt acquired delivery credit")
			}
		})
	}
}

// TestOperationEffectAcceptedReadbackRequiresFreshCredit validates reconciliation without allowing a second effect.
// TestOperationEffectAcceptedReadbackRequiresFreshCredit проверяет reconciliation без допуска второго effect.
func TestOperationEffectAcceptedReadbackRequiresFreshCredit(t *testing.T) {
	for _, scenario := range []string{"valid", "fresh-effect", "expired", "wide-credit", "unknown", "digest", "outcome", "reason", "duplicate-json", "oversized", "historical-metadata"} {
		t.Run(scenario, func(t *testing.T) {
			owner, client, binding, opening, completion := operationEffectFixture()
			client.beginEffect.ReconcileOnly = true
			client.beginEffect.ExpiresAt = timestamppb.New(time.Now().Add(20 * time.Second))
			accepted := &cp.ManagedKubernetesAcceptedEffectResult{Outcome: cp.ManagedKubernetesEffectOutcome_MANAGED_KUBERNETES_EFFECT_OUTCOME_SUCCEEDED, ResultJson: bytes.Clone(completion.ResultJSON), ResultDigest: client.completeEffect.ResultDigest}
			client.beginEffect.AcceptedResult = accepted
			switch scenario {
			case "fresh-effect":
				client.beginEffect.ReconcileOnly = false
			case "expired":
				client.beginEffect.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
			case "wide-credit":
				client.beginEffect.ExpiresAt = timestamppb.New(time.Now().Add(45 * time.Second))
			case "unknown":
				accepted.ProtoReflect().SetUnknown([]byte{0x78, 1})
			case "digest":
				accepted.ResultDigest = strings.Repeat("d", 64)
			case "outcome":
				accepted.Outcome = 0
			case "reason":
				accepted.ReasonCode = "UNREGISTERED_REASON"
			case "duplicate-json":
				accepted.ResultJson = []byte(`{"a":1,"a":2}`)
				digest := sha256.Sum256(accepted.ResultJson)
				accepted.ResultDigest = hex.EncodeToString(digest[:])
			case "oversized":
				accepted.ResultJson = []byte(strings.Repeat(" ", managedkubernetes.MaximumEffectResultBytes) + "{}")
				digest := sha256.Sum256(accepted.ResultJson)
				accepted.ResultDigest = hex.EncodeToString(digest[:])
			case "historical-metadata":
				client.beginEffect.AcceptedResult = nil
				client.beginEffect.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
			}
			admission, err := owner.BeginKubernetesEffect(t.Context(), binding, opening)
			if client.beginEffectCalls != 1 {
				t.Fatal("readback retried admission")
			}
			if scenario == "historical-metadata" {
				if err != nil || !admission.ReconcileOnly || admission.AcceptedResult != nil || admission.Stream != nil {
					t.Fatal("historical metadata acquired output credit")
				}
				return
			}
			if scenario != "valid" {
				if err == nil || admission.EffectRef != "" || admission.AcceptedResult != nil {
					t.Fatal("invalid readback acquired output credit")
				}
				return
			}
			if err != nil || !admission.ReconcileOnly || admission.Stream != nil || admission.AcceptedResult == nil || admission.AcceptedResult.Outcome != "SUCCEEDED" || !bytes.Equal(admission.AcceptedResult.ResultJSON, completion.ResultJSON) {
				t.Fatal("valid readback lost exact result or created dispatch credit")
			}
			accepted.ResultJson[0] = '!'
			if admission.AcceptedResult.ResultJSON[0] != '{' {
				t.Fatal("readback retained mutable protobuf bytes")
			}
		})
	}
}

// TestOperationEffectStreamRequiresHardCeiling refuses missing immutable lifetime before owner admission.
// TestOperationEffectStreamRequiresHardCeiling отклоняет отсутствие immutable lifetime до owner admission.
func TestOperationEffectStreamRequiresHardCeiling(t *testing.T) {
	owner, client, binding, opening, _ := operationEffectFixture()
	opening.Operation = "kubernetes.exec"
	opening.HardStreamDeadline = time.Time{}
	if _, err := owner.BeginKubernetesEffect(t.Context(), binding, opening); err == nil || client.beginEffectCalls != 0 {
		t.Fatal("stream without hard ceiling reached admission")
	}
}
