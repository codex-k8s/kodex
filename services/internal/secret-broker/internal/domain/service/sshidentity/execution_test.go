package sshidentity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	sshrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/sshtarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// executionPorts implements controlled owner/material/native ports around the actual orchestration; it does not claim a PostgreSQL or SSH transport proof.
// executionPorts реализует controlled owner/material/native ports вокруг actual orchestration; PostgreSQL либо SSH transport proof не заявляется.
type executionPorts struct {
	admission                               managedssh.ExecutionAdmission
	binding                                 managedssh.Binding
	receipt                                 managedssh.ExecutionReceipt
	begins, reads, natives, gets, completes int
	mode                                    string
	observedCleanup                         bool
	key                                     []byte
}

// executionDigest computes the independent expected integrity pin for synthetic input and output bytes.
// executionDigest вычисляет independent expected integrity pin synthetic input и output bytes.
func executionDigest(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

// newExecutionPorts creates bounded original coordinates without assigning a live actor or provider permission.
// newExecutionPorts создаёт bounded original coordinates без live actor либо provider permission.
func newExecutionPorts(t *testing.T, mode string) (*ExecutionService, *executionPorts) {
	t.Helper()
	input := []byte(`{}`)
	ref := "bri_execution_fixture"
	key := []byte("synthetic-private-key-bytes")
	f := &executionPorts{mode: mode, key: key, binding: managedssh.Binding{WorkloadInstance: "gateway_fixture", InvocationRef: "inv_execution_fixture", LeaseRef: "lease_fixture", Fence: "fence_fixture", Generation: 1, InputDigest: executionDigest(input)}}
	f.admission = managedssh.ExecutionAdmission{AttemptRef: "sxe_execution_fixture", InvocationRef: f.binding.InvocationRef, SnapshotDigest: strings.Repeat("a", 64), Operation: "ssh.identity.inspect", InputDigest: f.binding.InputDigest, SubjectDigest: strings.Repeat("b", 64), Mode: managedssh.ExecuteTransport, CanonicalInput: input, ExpiresAt: time.Now().Add(25 * time.Second),
		Identity: managedssh.IdentityLocator{Purpose: managedssh.IdentityPurpose, IdentityRevisionRef: ref, Revision: 1, TargetRef: "target_fixture", Namespace: "kodex-ssh-identities", SecretName: managedssh.IdentitySecretName(ref, 1), SecretUID: "synthetic-uid", SecretResourceVersion: "7", ContentSHA256: executionDigest(key), AuthenticationSHA256: strings.Repeat("c", 64)}}
	service, err := NewExecutionService(f, f, f, f)
	if err != nil {
		t.Fatal(err)
	}
	return service, f
}

// BeginSSHExecution returns a copy of one original admission or a lost ACK; caller cannot retry native work through this fixture.
// BeginSSHExecution возвращает copy original admission либо lost ACK; caller не получает retry native work.
func (f *executionPorts) BeginSSHExecution(context.Context, managedssh.Binding) (managedssh.ExecutionAdmission, error) {
	f.begins++
	if f.mode == "lost-begin" {
		return managedssh.ExecutionAdmission{}, sshrepo.ErrIdentityUnavailable
	}
	result := f.admission
	result.CanonicalInput = append([]byte(nil), result.CanonicalInput...)
	if f.mode == "bad-begin" {
		result.Identity.SecretUID = ""
	}
	return result, nil
}

// ReadSSHIdentity supplies owned synthetic bytes only after Begin; those bytes are checked for clearing once the operation joins.
// ReadSSHIdentity выдаёт owned synthetic bytes лишь после Begin; после operation join проверяется очистка bytes.
func (f *executionPorts) ReadSSHIdentity(context.Context, value.SSHIdentityLocator) ([]byte, error) {
	f.reads++
	return f.key, nil
}

// PrepareSSHIdentity derives only expected synthetic integrity metadata and confers no target permissions.
// PrepareSSHIdentity выводит лишь expected synthetic integrity metadata без target permissions.
func (f *executionPorts) PrepareSSHIdentity(raw []byte) (value.SSHIdentityKeyMetadata, error) {
	return value.SSHIdentityKeyMetadata{ContentSHA256: executionDigest(raw), AuthenticationSHA256: f.admission.Identity.AuthenticationSHA256}, nil
}

// GetSSHExecution exposes a recorded metadata receipt or the exact original deadline, and can inject owner infrastructure failure before Start.
// GetSSHExecution выдаёт recorded metadata receipt либо exact original deadline и может воспроизвести owner infrastructure failure перед Start.
func (f *executionPorts) GetSSHExecution(context.Context, managedssh.Binding) (managedssh.ExecutionReceipt, error) {
	f.gets++
	if f.mode == "owner-unavailable" {
		return managedssh.ExecutionReceipt{}, sshrepo.ErrIdentityUnavailable
	}
	if f.receipt.Recorded {
		result := f.receipt
		result.Observation.Result = nil
		return result, nil
	}
	return managedssh.ExecutionReceipt{AttemptRef: f.admission.AttemptRef, SnapshotDigest: f.admission.SnapshotDigest, State: "UNREPORTED", OwnerState: "RUNNING", AuthorizedUntil: f.admission.ExpiresAt}, nil
}

// ExecuteSSHIdentity invokes both pre-dispatch owner checks before returning joined synthetic output, with no background work or retry.
// ExecuteSSHIdentity вызывает оба pre-dispatch owner checks до возврата joined synthetic output, без background work либо retry.
func (f *executionPorts) ExecuteSSHIdentity(ctx context.Context, _ managedssh.ExecutionAdmission, _ []byte, check func(context.Context) error) (managedssh.ExecutionObservation, error) {
	f.natives++
	if err := check(ctx); err != nil {
		return managedssh.ExecutionObservation{}, err
	}
	if err := check(ctx); err != nil {
		return managedssh.ExecutionObservation{}, err
	}
	raw := []byte(`{"status":"observed"}`)
	return managedssh.ExecutionObservation{Outcome: managedssh.ExecutionSucceeded, Result: raw, ResultDigest: executionDigest(raw)}, nil
}

// CompleteSSHExecution commits one synthetic immutable observation before optionally losing its ACK; invalid-success refusal remains a separate injected outcome.
// CompleteSSHExecution фиксирует synthetic immutable observation до optional lost ACK; invalid-success refusal остаётся отдельным исходом.
func (f *executionPorts) CompleteSSHExecution(ctx context.Context, _ managedssh.Binding, admission managedssh.ExecutionAdmission, observation managedssh.ExecutionObservation) (managedssh.ExecutionReceipt, error) {
	f.completes++
	deadline, ok := ctx.Deadline()
	f.observedCleanup = ctx.Err() == nil && ok && time.Until(deadline) <= 3*time.Second
	f.receipt = managedssh.ExecutionReceipt{AttemptRef: admission.AttemptRef, SnapshotDigest: admission.SnapshotDigest, State: string(observation.Outcome), OwnerState: string(observation.Outcome), Recorded: true, Applied: true,
		Observation: managedssh.ExecutionObservation{Outcome: observation.Outcome, Result: append([]byte(nil), observation.Result...), ResultDigest: observation.ResultDigest}}
	if observation.Outcome == managedssh.ExecutionUnknown {
		f.receipt.State = "UNKNOWN_OUTCOME"
		f.receipt.OwnerState = "UNKNOWN_OUTCOME"
	}
	if f.mode == "lost-complete" {
		return managedssh.ExecutionReceipt{}, sshrepo.ErrIdentityUnavailable
	}
	if f.mode == "bad-complete" {
		return managedssh.ExecutionReceipt{}, sshrepo.ErrIdentityRejected
	}
	return f.receipt, nil
}

// TestSSHExecutionOrchestrationPreservesOneAttemptAndOwnerFaults exercises lost ACK and damaged ACK separately and proves owner infrastructure failure cannot become terminal completion.
// TestSSHExecutionOrchestrationPreservesOneAttemptAndOwnerFaults раздельно проверяет lost ACK и damaged ACK и доказывает отсутствие terminal completion при owner infrastructure failure.
func TestSSHExecutionOrchestrationPreservesOneAttemptAndOwnerFaults(t *testing.T) {
	for _, mode := range []string{"success", "lost-begin", "bad-begin", "owner-unavailable", "lost-complete", "bad-complete"} {
		t.Run(mode, func(t *testing.T) {
			service, f := newExecutionPorts(t, mode)
			receipt, err := service.Execute(t.Context(), f.binding)
			if f.begins != 1 || f.natives > 1 || f.completes > 1 {
				t.Fatal("work was retried")
			}
			switch mode {
			case "success":
				if err != nil || !receipt.Applied || len(receipt.Observation.Result) == 0 || f.gets != 2 || f.completes != 1 || !f.observedCleanup {
					t.Fatal("exact applied result lost", err)
				}
			case "lost-begin", "bad-begin":
				if err == nil || f.reads != 0 || f.natives != 0 || f.completes != 0 || f.gets != 0 {
					t.Fatal("uncertain admission consumed material or dispatch")
				}
			case "owner-unavailable":
				if !errors.Is(err, sshrepo.ErrIdentityUnavailable) || f.completes != 0 {
					t.Fatal("owner outage became terminal transition", err)
				}
			case "lost-complete":
				if err != nil || !receipt.Recorded || !receipt.Applied || len(receipt.Observation.Result) != 0 || f.gets != 3 || f.completes != 1 {
					t.Fatal("lost ACK repeated work or exposed new output", err)
				}
			case "bad-complete":
				if !errors.Is(err, sshrepo.ErrIdentityRejected) || f.gets != 2 {
					t.Fatal("damaged successful ACK gained metadata recovery", err)
				}
			}
			if f.reads > 0 {
				for _, value := range f.key {
					if value != 0 {
						t.Fatal("private key copy survived joined operation")
					}
				}
			}
		})
	}
}
