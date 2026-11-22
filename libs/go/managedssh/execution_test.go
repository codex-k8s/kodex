package managedssh

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"
)

// executionAdmissionFixture supplies structural synthetic owner coordinates only; it grants no permission or actual target access.
// executionAdmissionFixture задаёт лишь structural synthetic owner coordinates без permission либо actual target access.
func executionAdmissionFixture() ExecutionAdmission {
	identity := IdentityLocator{Purpose: IdentityPurpose, IdentityRevisionRef: "identity_revision_fixture", TargetRef: "target_ssh_fixture", Revision: 1, Namespace: "kodex-ssh-identities", SecretUID: "uid-fixture", SecretResourceVersion: "rv-fixture", ContentSHA256: strings.Repeat("a", 64), AuthenticationSHA256: strings.Repeat("b", 64)}
	identity.SecretName = IdentitySecretName(identity.IdentityRevisionRef, identity.Revision)
	raw := []byte(`{"command":"private-command","maximum_output":1024}`)
	digest := sha256.Sum256(raw)
	return ExecutionAdmission{AttemptRef: "attempt_ssh_fixture", InvocationRef: "invocation_ssh_fixture", SnapshotDigest: strings.Repeat("c", 64), Operation: "ssh.execute", CanonicalInput: raw, InputDigest: hex.EncodeToString(digest[:]), SubjectDigest: strings.Repeat("d", 64), Identity: identity, Mode: ExecuteTransport, ExpiresAt: time.Now().Add(20 * time.Second)}
}

// TestSSHExecutionAdmissionRejectsMixedSource verifies current and original input are independently closed, while arbitrary shell remains a separate family even when it resembles a read command.
// TestSSHExecutionAdmissionRejectsMixedSource проверяет independent closed current и original input; arbitrary shell остаётся отдельным family даже при сходстве с read command.
func TestSSHExecutionAdmissionRejectsMixedSource(t *testing.T) {
	valid := executionAdmissionFixture()
	if valid.Validate(time.Now()) != nil {
		t.Fatal("complete structural execution admission rejected")
	}
	for _, change := range []func(*ExecutionAdmission){
		func(a *ExecutionAdmission) { a.Mode = ManagedTransport },
		func(a *ExecutionAdmission) { a.InvocationRef = "" },
		func(a *ExecutionAdmission) { a.Operation = "ssh.system.read" },
		func(a *ExecutionAdmission) { a.InputDigest = strings.Repeat("e", 64) },
		func(a *ExecutionAdmission) { a.SubjectDigest = "" },
		func(a *ExecutionAdmission) { a.ExpiresAt = time.Now().Add(time.Minute) },
		func(a *ExecutionAdmission) { a.Identity.Purpose = "TOKEN" },
		func(a *ExecutionAdmission) { a.ReceiptSource = &ManagedReceiptSource{InvocationRef: a.InvocationRef} },
	} {
		broken := executionAdmissionFixture()
		change(&broken)
		if broken.Validate(time.Now()) == nil {
			t.Fatal("mixed execution source admitted")
		}
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", valid, valid), "private-command") {
		t.Fatal("private command escaped diagnostic boundary")
	}
}

// TestSSHExecutionObservationRetainsUncertainty verifies that UNKNOWN has no output channel, all observed output is integrity bound, and non-success partial data is not silently accepted without a digest.
// TestSSHExecutionObservationRetainsUncertainty проверяет отсутствие output channel у UNKNOWN, integrity binding observed output и отказ non-success partial data без digest.
func TestSSHExecutionObservationRetainsUncertainty(t *testing.T) {
	raw := []byte(`{"private":"result"}`)
	digest := sha256.Sum256(raw)
	for _, outcome := range []ExecutionOutcome{ExecutionSucceeded, ExecutionFailed} {
		observation := ExecutionObservation{Outcome: outcome, Result: raw, ResultDigest: hex.EncodeToString(digest[:])}
		if observation.Validate() != nil {
			t.Fatal("bounded observed output rejected")
		}
		observation.ResultDigest = strings.Repeat("f", 64)
		if observation.Validate() == nil {
			t.Fatal("altered output digest accepted")
		}
	}
	for _, observation := range []ExecutionObservation{
		{Outcome: ExecutionUnknown, Result: raw, ResultDigest: hex.EncodeToString(digest[:])},
		{Outcome: ExecutionSucceeded}, {Outcome: ExecutionFailed, Result: raw}, {Outcome: "NOT_STARTED"},
	} {
		if observation.Validate() == nil {
			t.Fatal("mixed or incomplete observation accepted")
		}
	}
	if (ExecutionObservation{Outcome: ExecutionUnknown}).Validate() != nil || (ExecutionObservation{Outcome: ExecutionFailed}).Validate() != nil {
		t.Fatal("closed empty uncertainty or refusal rejected")
	}
	if strings.Contains(fmt.Sprintf("%#v", ExecutionObservation{Outcome: ExecutionSucceeded, Result: raw}), "result") {
		t.Fatal("private result escaped diagnostics")
	}
}
