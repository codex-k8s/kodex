package grpc

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
)

// TestManagedKubernetesEffectReceiptSeparatesSubmissionAndOutput verifies explicit discarded READ
// acknowledgement without payload credit, and refuses UNKNOWN promotion, expired or widened credit.
// TestManagedKubernetesEffectReceiptSeparatesSubmissionAndOutput проверяет explicit discarded READ
// acknowledgement без payload credit и запрещает UNKNOWN promotion, expired либо widened credit.
func TestManagedKubernetesEffectReceiptSeparatesSubmissionAndOutput(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	raw := []byte(`{"metadata":{"name":"synthetic"}}`)
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	accepted := entity.ManagedKubernetesEffectReceipt{State: "SUCCEEDED", ResultDigest: digest, SubmissionDigest: digest, Applied: true, AuthorizedUntil: now.Add(10 * time.Second)}
	reply := managedKubernetesEffectReceiptToProto(accepted, digest, enum.KubernetesEffectSucceeded, now)
	if reply == nil || !reply.GetApplied() || reply.GetSubmissionDigest() != digest || reply.GetResultDigest() != digest || reply.GetAuthorizedUntil().AsTime() != accepted.AuthorizedUntil {
		t.Fatal("fresh accepted output lost exact receipt")
	}
	for _, change := range []func(*entity.ManagedKubernetesEffectReceipt){
		func(input *entity.ManagedKubernetesEffectReceipt) { input.SubmissionDigest = strings.Repeat("a", 64) },
		func(input *entity.ManagedKubernetesEffectReceipt) { input.ResultDigest = strings.Repeat("a", 64) },
		func(input *entity.ManagedKubernetesEffectReceipt) { input.AuthorizedUntil = now },
		func(input *entity.ManagedKubernetesEffectReceipt) { input.AuthorizedUntil = now.Add(31 * time.Second) },
		func(input *entity.ManagedKubernetesEffectReceipt) { input.Applied = false },
		func(input *entity.ManagedKubernetesEffectReceipt) { input.State = "UNREGISTERED" },
	} {
		changed := accepted
		change(&changed)
		if managedKubernetesEffectReceiptToProto(changed, digest, enum.KubernetesEffectSucceeded, now) != nil {
			t.Fatal("invalid owner result acquired output credit")
		}
	}
	if managedKubernetesEffectReceiptToProto(accepted, digest, enum.KubernetesEffectUnknown, now) != nil {
		t.Fatal("UNKNOWN submission was promoted by matching bytes")
	}
	discarded := entity.ManagedKubernetesEffectReceipt{State: "CANCELLED", ResultDigest: kubernetesEmptyAcceptedResultSHA256, SubmissionDigest: digest}
	reply = managedKubernetesEffectReceiptToProto(discarded, digest, enum.KubernetesEffectSucceeded, now)
	if reply == nil || reply.GetApplied() || reply.GetAuthorizedUntil() != nil || reply.GetResultDigest() == reply.GetSubmissionDigest() {
		t.Fatal("discarded READ lost submission ACK or gained output credit")
	}
	discarded.AuthorizedUntil = now.Add(time.Second)
	if managedKubernetesEffectReceiptToProto(discarded, digest, enum.KubernetesEffectSucceeded, now) != nil {
		t.Fatal("discarded READ retained delivery authority")
	}
}

// TestManagedKubernetesAcceptedResultRequiresFreshCurrentOutput rejects historical or corrupted
// readback bytes, including duplicate JSON, without manufacturing a new target effect admission.
// TestManagedKubernetesAcceptedResultRequiresFreshCurrentOutput отклоняет historical либо corrupted
// readback bytes, включая duplicate JSON, без создания нового target effect admission.
func TestManagedKubernetesAcceptedResultRequiresFreshCurrentOutput(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	raw := []byte(`{"items":[]}`)
	sum := sha256.Sum256(raw)
	accepted := entity.ManagedKubernetesAcceptedEffectResult{Outcome: enum.KubernetesEffectSucceeded, ResultJSON: raw, ResultDigest: hex.EncodeToString(sum[:])}
	reply := managedKubernetesAcceptedResultToProto(&accepted, now.Add(5*time.Second), now)
	if reply == nil || string(reply.GetResultJson()) != string(raw) {
		t.Fatal("current accepted result lost safe bytes")
	}
	reply.ResultJson[0] = '['
	if accepted.ResultJSON[0] != '{' {
		t.Fatal("response aliases immutable stored result")
	}
	if managedKubernetesAcceptedResultToProto(&accepted, now, now) != nil || managedKubernetesAcceptedResultToProto(&accepted, now.Add(time.Minute), now) != nil {
		t.Fatal("historical or widened readback acquired output credit")
	}
	accepted.ResultJSON = []byte(`{"items":[],"items":[]}`)
	sum = sha256.Sum256(accepted.ResultJSON)
	accepted.ResultDigest = hex.EncodeToString(sum[:])
	if managedKubernetesAcceptedResultToProto(&accepted, now.Add(time.Second), now) != nil {
		t.Fatal("ambiguous accepted JSON crossed output boundary")
	}
}
