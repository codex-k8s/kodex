package sshidentity

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"strings"
	"testing"
	"time"

	sshrepository "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/sshtarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/integration/sshtarget"
	"golang.org/x/crypto/ssh"
)

// setupOwner models only the admitted owner transaction boundary for broker tests; it deliberately refuses observation of an abandoned WRITE without a committed explicit UNKNOWN receipt.
// setupOwner моделирует только admitted owner transaction boundary для broker tests; он намеренно запрещает observation abandoned WRITE без committed explicit UNKNOWN receipt.
type setupOwner struct {
	snapshot                            value.SSHIdentityUploadSnapshot
	permitDigest                        string
	spent, eligible, terminal, attached bool
	generation                          int64
	claims, completions                 int
	failBefore, failAfter               bool
	change                              func(*value.SSHIdentityUploadAdmission)
	beforeResolve                       func()
	last                                sshrepository.IdentityUploadCompletion
	completionContextError              error
}

// ResolveSSHIdentityUpload exposes only fresh eligible state, keeping terminal and unreported attempts closed even after their handler disappeared.
// ResolveSSHIdentityUpload предоставляет только fresh eligible state, сохраняя terminal и unreported attempts закрытыми даже после исчезновения handler.
func (owner *setupOwner) ResolveSSHIdentityUpload(_ context.Context, ref, permit string) (value.SSHIdentityUploadSnapshot, error) {
	if owner.beforeResolve != nil {
		owner.beforeResolve()
	}
	if ref != owner.snapshot.SetupRef || permit != owner.permitDigest || owner.terminal || owner.spent && !owner.eligible {
		return value.SSHIdentityUploadSnapshot{}, errors.New("synthetic owner refused")
	}
	snapshot := owner.snapshot
	if owner.spent {
		snapshot.Mode = value.SSHIdentityUploadObserve
	}
	return snapshot, nil
}

// ClaimSSHIdentityUpload records a single WRITE before returning its fence and otherwise allows only an explicitly receipt-backed observation generation.
// ClaimSSHIdentityUpload сохраняет single WRITE до возврата fence и иначе допускает только explicitly receipt-backed observation generation.
func (owner *setupOwner) ClaimSSHIdentityUpload(_ context.Context, input sshrepository.IdentityUploadClaim) (value.SSHIdentityUploadAdmission, error) {
	owner.claims++
	if input.SetupRef != owner.snapshot.SetupRef || input.PermitSHA256 != owner.permitDigest || input.SnapshotDigest != owner.snapshot.Digest || owner.terminal || owner.spent && !owner.eligible {
		return value.SSHIdentityUploadAdmission{}, errors.New("synthetic claim refused")
	}
	mode := value.SSHIdentityUploadWrite
	if owner.spent {
		mode = value.SSHIdentityUploadObserve
	}
	owner.spent = true
	owner.snapshot.AuthenticationSHA256 = input.Key.AuthenticationSHA256
	owner.generation++
	provision, ok := owner.snapshot.Preparation.Provision(input.Key)
	if !ok {
		return value.SSHIdentityUploadAdmission{}, errors.New("synthetic key mismatch")
	}
	admission := value.SSHIdentityUploadAdmission{SetupRef: input.SetupRef, SnapshotDigest: input.SnapshotDigest, AttemptRef: "ssa_fixture_attempt", WorkloadInstance: input.WorkloadInstance, Fence: "fence_fixture_private", Generation: owner.generation, Mode: mode, Provision: provision, ExpiresAt: time.Now().Add(20 * time.Second)}
	if owner.change != nil {
		owner.change(&admission)
	}
	return admission, nil
}

// CompleteSSHIdentityUpload simulates commit-before-ACK and lost-before-commit separately; only an actual explicit completion may change later admission eligibility.
// CompleteSSHIdentityUpload отдельно моделирует commit-before-ACK и lost-before-commit; только actual explicit completion меняет дальнейшую admission eligibility.
func (owner *setupOwner) CompleteSSHIdentityUpload(ctx context.Context, input sshrepository.IdentityUploadCompletion) error {
	owner.completions++
	owner.completionContextError = ctx.Err()
	if owner.failBefore {
		return errors.New("synthetic completion lost before commit")
	}
	owner.last = input
	owner.eligible = input.Unknown
	owner.terminal = input.Rejected != nil || input.Identity != nil
	owner.attached = input.Identity != nil
	if owner.failAfter {
		return errors.New("synthetic completion ACK lost after commit")
	}
	return nil
}

// setupStore counts purpose effects and can cancel the caller after its single write, returning either known immutable pins or the configured classified outcome without retries.
// setupStore считает purpose effects и может отменить caller после single write, возвращая known immutable pins либо configured classified outcome без retries.
type setupStore struct {
	writes, reads int
	writeErr      error
	cancel        context.CancelFunc
}

// CreateSSHIdentity performs one synthetic effect and returns exact original pins; cancellation happens only after that effect so independent evidence delivery is exercised.
// CreateSSHIdentity выполняет один synthetic effect и возвращает exact original pins; cancellation происходит только после effect для проверки independent evidence delivery.
func (store *setupStore) CreateSSHIdentity(_ context.Context, provision value.SSHIdentityProvision, _ []byte) (value.SSHIdentityLocator, error) {
	store.writes++
	if store.cancel != nil {
		store.cancel()
	}
	if store.writeErr != nil {
		return value.SSHIdentityLocator{}, store.writeErr
	}
	return setupLocator(provision), nil
}

// ObserveSSHIdentity returns the original intent without spending create credit or changing any material metadata.
// ObserveSSHIdentity возвращает original intent без расходования create credit либо изменения material metadata.
func (store *setupStore) ObserveSSHIdentity(_ context.Context, provision value.SSHIdentityProvision) (value.SSHIdentityLocator, error) {
	store.reads++
	return setupLocator(provision), nil
}

// ReadSSHIdentity is intentionally unavailable because setup must never fall back to the bound runtime credential reader.
// ReadSSHIdentity намеренно unavailable, поскольку setup не должен переходить к bound runtime credential reader.
func (*setupStore) ReadSSHIdentity(context.Context, value.SSHIdentityLocator) ([]byte, error) {
	return nil, sshrepository.ErrIdentityUnavailable
}

// DeleteSSHIdentity is intentionally unavailable because upload completion grants no cleanup effect.
// DeleteSSHIdentity намеренно unavailable, поскольку upload completion не разрешает cleanup effect.
func (*setupStore) DeleteSSHIdentity(context.Context, value.SSHIdentityLocator) error {
	return sshrepository.ErrIdentityRejected
}

// setupLocator constructs synthetic private pins for a known result without putting key bytes in a receipt or assertion output.
// setupLocator создаёт synthetic private pins known result без key bytes в receipt либо assertion output.
func setupLocator(provision value.SSHIdentityProvision) value.SSHIdentityLocator {
	return value.SSHIdentityLocator{Purpose: provision.Purpose, IdentityRevisionRef: provision.IdentityRevisionRef, TargetRef: provision.TargetRef, Revision: provision.Revision, Namespace: provision.Namespace, ContentSHA256: provision.ContentSHA256, AuthenticationSHA256: provision.AuthenticationSHA256, SecretName: value.SSHIdentitySecretName(provision.IdentityRevisionRef, provision.Revision), SecretUID: "synthetic-object-uid", SecretResourceVersion: "rv1"}
}

// newSetupFixture generates an ephemeral real Ed25519 key and pairs the actual production parser with an owner transaction model; no live credentials or network are used.
// newSetupFixture создаёт ephemeral real Ed25519 key и связывает actual production parser с owner transaction model; live credentials и network отсутствуют.
func newSetupFixture(t *testing.T) (*Service, *setupOwner, *setupStore, []byte, []byte) {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal("generate synthetic key")
	}
	block, err := ssh.MarshalPrivateKey(key, "")
	if err != nil {
		t.Fatal("encode synthetic key")
	}
	material := pem.EncodeToMemory(block)
	t.Cleanup(func() { clear(material); clear(key) })
	permit := bytes.Repeat([]byte{1}, 32)
	permitDigest := sha256.Sum256(permit)
	content := sha256.Sum256(material)
	owner := &setupOwner{permitDigest: hex.EncodeToString(permitDigest[:]), snapshot: value.SSHIdentityUploadSnapshot{SetupRef: "ssu_fixture_setup", Digest: strings.Repeat("a", 64), Mode: value.SSHIdentityUploadWrite, Preparation: value.SSHIdentityPreparation{IdentityRevisionRef: "bri_fixture_revision", TargetRef: "ssh_fixture_target", Revision: 1, Namespace: "synthetic-ssh", ContentSHA256: hex.EncodeToString(content[:])}, ContentBytes: uint32(len(material)), ExpiresAt: time.Now().Add(60 * time.Second)}}
	store := &setupStore{}
	service, err := New(owner, sshtarget.KeyPreparer{}, store, "pod_fixture_instance", "synthetic-ssh")
	if err != nil {
		t.Fatal("compose setup fixture")
	}
	return service, owner, store, permit, material
}

// TestSetupReceiptControlsRecovery checks known, explicit UNKNOWN, terminal rejected and missing-completion branches across a fresh service instance; observing or recreating abandoned/rejected writes is forbidden.
// TestSetupReceiptControlsRecovery проверяет known, explicit UNKNOWN, terminal rejected и missing-completion branches через fresh service instance; observation либо recreation abandoned/rejected writes запрещены.
func TestSetupReceiptControlsRecovery(t *testing.T) {
	for _, mode := range []string{"known", "unknown", "rejected", "unreported rejected", "committed rejected lost ACK"} {
		t.Run(mode, func(t *testing.T) {
			service, owner, store, permit, material := newSetupFixture(t)
			if mode == "unknown" {
				store.writeErr = sshrepository.ErrIdentityUnknown
			}
			if strings.Contains(mode, "rejected") {
				store.writeErr = &sshrepository.IdentityAcknowledgementRejection{ResponseSHA256: strings.Repeat("b", 64), ObservedUID: "original-ack-uid"}
			}
			owner.failBefore = mode == "unreported rejected"
			owner.failAfter = mode == "committed rejected lost ACK"
			err := service.Materialize(t.Context(), owner.snapshot.SetupRef, permit, material)
			if (mode == "known") != (err == nil) || store.writes != 1 || owner.claims != 1 || owner.completions != 1 {
				t.Fatal("one upload did not produce exactly one admitted write and completion")
			}
			if mode == "rejected" && !errors.Is(err, sshrepository.ErrIdentityAcknowledgementRejected) || mode == "unknown" && !errors.Is(err, sshrepository.ErrIdentityUnknown) {
				t.Fatal("classified receipt outcome lost")
			}
			owner.failBefore, owner.failAfter = false, false
			fresh, _ := New(owner, sshtarget.KeyPreparer{}, store, "pod_fresh_instance", "synthetic-ssh")
			observeErr := fresh.Observe(t.Context(), owner.snapshot.SetupRef, permit)
			if (mode == "unknown") != (observeErr == nil) || store.writes != 1 || (mode == "unknown") != (store.reads == 1) {
				t.Fatal("abandoned or terminal receipt gained observation credit")
			}
			if fresh.Materialize(t.Context(), owner.snapshot.SetupRef, permit, material) == nil || store.writes != 1 {
				t.Fatal("setup gained a second material write")
			}
		})
	}
}

// TestSetupRejectsChangedOwnerAdmissionBeforeStorage checks whole-binding substitutions, mixed mode, deadlines and malformed owner replies rather than just validating each DTO in isolation.
// TestSetupRejectsChangedOwnerAdmissionBeforeStorage проверяет whole-binding substitutions, mixed mode, deadlines и malformed owner replies, а не только каждый DTO отдельно.
func TestSetupRejectsChangedOwnerAdmissionBeforeStorage(t *testing.T) {
	mutations := []func(*value.SSHIdentityUploadAdmission){
		func(a *value.SSHIdentityUploadAdmission) { a.Provision.TargetRef = "foreign_target" },
		func(a *value.SSHIdentityUploadAdmission) { a.Provision.Namespace = "foreign-namespace" },
		func(a *value.SSHIdentityUploadAdmission) { a.Provision.AuthenticationSHA256 = strings.Repeat("f", 64) },
		func(a *value.SSHIdentityUploadAdmission) { a.SnapshotDigest = strings.Repeat("f", 64) },
		func(a *value.SSHIdentityUploadAdmission) { a.Mode = value.SSHIdentityUploadObserve },
		func(a *value.SSHIdentityUploadAdmission) { a.WorkloadInstance = "foreign_instance" },
		func(a *value.SSHIdentityUploadAdmission) { a.Fence = "" },
		func(a *value.SSHIdentityUploadAdmission) { a.Generation = 9 },
		func(a *value.SSHIdentityUploadAdmission) { a.ExpiresAt = time.Now().Add(-time.Second) },
		func(a *value.SSHIdentityUploadAdmission) { a.ExpiresAt = time.Now().Add(time.Minute) },
	}
	for index, mutate := range mutations {
		service, owner, store, permit, material := newSetupFixture(t)
		owner.change = mutate
		if service.Materialize(t.Context(), owner.snapshot.SetupRef, permit, material) == nil || store.writes != 0 || owner.completions != 0 {
			t.Fatal("changed owner admission reached storage", index)
		}
	}
}

// TestSetupSnapshotsKeyBeforeOwnerIOAndRecordsAfterCancellation ensures a caller cannot swap a key during preflight and cancellation after an effect cannot erase its owner evidence.
// TestSetupSnapshotsKeyBeforeOwnerIOAndRecordsAfterCancellation проверяет, что caller не подменяет key во время preflight, а cancellation после effect не стирает owner evidence.
func TestSetupSnapshotsKeyBeforeOwnerIOAndRecordsAfterCancellation(t *testing.T) {
	service, owner, store, permit, material := newSetupFixture(t)
	owner.beforeResolve = func() { clear(material) }
	ctx, cancel := context.WithCancel(t.Context())
	store.cancel = cancel
	if service.Materialize(ctx, owner.snapshot.SetupRef, permit, material) != nil || store.writes != 1 || owner.completions != 1 || owner.completionContextError != nil || !owner.attached {
		t.Fatal("key mutation or cancelled caller corrupted admitted evidence")
	}
}
