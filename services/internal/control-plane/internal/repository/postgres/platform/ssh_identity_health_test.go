package platform

import (
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"strings"
	"testing"
	"time"
)

// TestSSHHealthBindingRetainsOriginalOpaqueSnapshot verifies every claim coordinate and the strict owner deadline without deriving a digest from empty operation input.
// TestSSHHealthBindingRetainsOriginalOpaqueSnapshot проверяет каждый claim coordinate и strict owner deadline без digest от empty operation input.
func TestSSHHealthBindingRetainsOriginalOpaqueSnapshot(t *testing.T) {
	now := time.Now()
	expiry := now.Add(time.Minute)
	binding := managedssh.Binding{WorkloadInstance: "gateway_fixture", ConnectionTestRef: "test_fixture", LeaseRef: "lease_fixture", Fence: "fence_fixture", Generation: 1, InputDigest: strings.Repeat("a", 64)}
	job := sshHealthJob{ref: binding.ConnectionTestRef, leaseRef: binding.LeaseRef, fenceDigest: sshHealthHash([]byte(binding.Fence)), instance: binding.WorkloadInstance, inputDigest: binding.InputDigest, generation: 1, expiresAt: &expiry}
	if !job.matches(binding, now) {
		t.Fatal("original opaque snapshot rejected")
	}
	for _, mutate := range []func(*managedssh.Binding){func(b *managedssh.Binding) { b.InputDigest = strings.Repeat("b", 64) }, func(b *managedssh.Binding) { b.WorkloadInstance = "foreign_gateway" }, func(b *managedssh.Binding) { b.LeaseRef = "foreign_lease" }, func(b *managedssh.Binding) { b.Fence = "foreign_fence" }, func(b *managedssh.Binding) { b.Generation = 2 }, func(b *managedssh.Binding) { b.InvocationRef = "invocation_fixture" }, func(b *managedssh.Binding) { b.ConnectionTestRef = "" }} {
		changed := binding
		mutate(&changed)
		if job.matches(changed, now) {
			t.Fatal("changed claim accepted")
		}
	}
	changedRef := binding
	changedRef.ConnectionTestRef = "foreign_test"
	if job.matches(changedRef, now) {
		t.Fatal("foreign test accepted")
	}
	if job.matches(binding, expiry) {
		t.Fatal("expired claim accepted")
	}
}

// TestSSHHealthCompletionClosedUnion rejects partial facts before any owner transition and preserves the difference between authentication and transport uncertainty.
// TestSSHHealthCompletionClosedUnion отвергает partial facts до owner transition и сохраняет различие authentication и transport uncertainty.
func TestSSHHealthCompletionClosedUnion(t *testing.T) {
	input := command.SSHIdentityHealthComplete{Binding: managedssh.Binding{WorkloadInstance: "gateway_fixture", ConnectionTestRef: "test_fixture", LeaseRef: "lease_fixture", Fence: "fence_fixture", Generation: 1, InputDigest: strings.Repeat("a", 64)}, AttemptRef: "probe_fixture", SnapshotDigest: strings.Repeat("b", 64), Outcome: entity.PersonalIntegrationCredentialAuthenticated, ObservedSubjectDigest: strings.Repeat("c", 64), QualifiedUntil: time.Unix(1900000000, 0)}
	if !validSSHHealthOwnerCompletion(input) {
		t.Fatal("complete authentication fact rejected")
	}
	for _, mutate := range []func(*command.SSHIdentityHealthComplete){func(i *command.SSHIdentityHealthComplete) { i.ObservedSubjectDigest = "" }, func(i *command.SSHIdentityHealthComplete) { i.QualifiedUntil = time.Time{} }, func(i *command.SSHIdentityHealthComplete) { i.QualifiedUntil = i.QualifiedUntil.Add(time.Nanosecond) }, func(i *command.SSHIdentityHealthComplete) { i.Outcome = "FUTURE" }, func(i *command.SSHIdentityHealthComplete) {
		i.Outcome = entity.PersonalIntegrationCredentialVerificationUnavailable
	}, func(i *command.SSHIdentityHealthComplete) { i.SnapshotDigest = "" }} {
		changed := input
		mutate(&changed)
		if validSSHHealthOwnerCompletion(changed) {
			t.Fatal("partial health fact accepted")
		}
	}
	input.Outcome = entity.PersonalIntegrationCredentialVerificationUnavailable
	input.ObservedSubjectDigest = ""
	input.QualifiedUntil = time.Time{}
	if !validSSHHealthOwnerCompletion(input) {
		t.Fatal("joined unavailable read rejected")
	}
}
