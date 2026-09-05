package secretdraft

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/secretdrafts"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

type ownerFixture struct {
	work                                value.DraftWork
	result                              value.DraftResult
	consumeErr, completeErr, cleanupErr error
	completed, failed, cleanup          int
	decision                            value.DraftRecoveryDecision
	lastEncrypted                       *value.DraftEncryptedDescriptor
	lastMaterialization                 *value.DraftMaterialization
}

func (owner *ownerFixture) Check(context.Context) error { return nil }
func (owner *ownerFixture) Consume(context.Context, string) (value.DraftWork, error) {
	return owner.work, owner.consumeErr
}
func (owner *ownerFixture) Complete(_ context.Context, _ value.DraftWork, encrypted *value.DraftEncryptedDescriptor, materialization *value.DraftMaterialization) (value.DraftResult, error) {
	owner.completed++
	owner.lastEncrypted, owner.lastMaterialization = encrypted, materialization
	return owner.result, owner.completeErr
}
func (owner *ownerFixture) Fail(context.Context, value.DraftWork) error { owner.failed++; return nil }
func (owner *ownerFixture) ListRecovery(context.Context) ([]value.DraftWork, error) {
	return []value.DraftWork{owner.work}, nil
}
func (owner *ownerFixture) Recover(_ context.Context, _ value.DraftWork, encrypted *value.DraftEncryptedDescriptor, materialization *value.DraftMaterialization) (value.DraftRecoveryDecision, error) {
	owner.lastEncrypted, owner.lastMaterialization = encrypted, materialization
	return owner.decision, nil
}
func (owner *ownerFixture) CompleteCleanup(context.Context, value.DraftWork, *value.DraftEncryptedDescriptor, *value.DraftMaterialization) error {
	owner.cleanup++
	return owner.cleanupErr
}

type cipherFixture struct {
	plaintext                []byte
	err                      error
	encryptions, decryptions int
}

func (cipher *cipherFixture) Encrypt(context.Context, value.SecretDraftBinding, []byte) (value.EncryptedSecretDraft, error) {
	cipher.encryptions++
	return value.EncryptedSecretDraft{Key: value.DraftEncryptionKey{ID: "fixture", Generation: 1}, Ciphertext: []byte("synthetic ciphertext")}, cipher.err
}
func (cipher *cipherFixture) Decrypt(context.Context, value.SecretDraftBinding, value.EncryptedSecretDraft) ([]byte, error) {
	cipher.decryptions++
	return bytes.Clone(cipher.plaintext), cipher.err
}

type encryptedFixture struct {
	descriptor              value.DraftEncryptedDescriptor
	err                     error
	creates, reads, deletes int
	absent                  bool
}

func (store *encryptedFixture) Check(context.Context) error { return store.err }
func (store *encryptedFixture) Create(context.Context, value.DraftWork, value.EncryptedSecretDraft) (value.DraftEncryptedDescriptor, error) {
	store.creates++
	return store.descriptor, store.err
}
func (store *encryptedFixture) Read(context.Context, value.DraftWork, value.DraftEncryptedDescriptor) (value.EncryptedSecretDraft, error) {
	store.reads++
	return value.EncryptedSecretDraft{}, store.err
}
func (store *encryptedFixture) Lookup(context.Context, value.DraftWork) (value.DraftEncryptedDescriptor, error) {
	if store.absent {
		return value.DraftEncryptedDescriptor{}, secretdrafts.ErrNotFound
	}
	return store.descriptor, store.err
}
func (store *encryptedFixture) Delete(context.Context, value.DraftWork, value.DraftEncryptedDescriptor) error {
	store.deletes++
	store.absent = true
	return store.err
}

type runtimeFixture struct {
	descriptor         value.DraftMaterialization
	err                error
	publishes, deletes int
	absent             bool
}

func (store *runtimeFixture) Publish(context.Context, value.DraftWork, []byte) (value.DraftMaterialization, error) {
	store.publishes++
	return store.descriptor, store.err
}
func (store *runtimeFixture) Lookup(context.Context, value.DraftWork) (value.DraftMaterialization, error) {
	if store.absent {
		return value.DraftMaterialization{}, secretdrafts.ErrNotFound
	}
	return store.descriptor, store.err
}
func (store *runtimeFixture) Delete(context.Context, value.DraftWork, value.DraftMaterialization) error {
	store.deletes++
	store.absent = true
	return store.err
}

func serviceFixture(t *testing.T, kind value.DraftOperation) (*Service, *ownerFixture, *encryptedFixture, *runtimeFixture, *cipherFixture) {
	t.Helper()
	plaintext := []byte("synthetic draft value")
	digest := sha256.Sum256(plaintext)
	now := time.Now()
	draft := value.SecretDraft{Ref: "drf_fixture", ProjectRef: "prj_fixture", SecretRef: "sec_fixture", Generation: 2, Version: 3,
		ValueType: "STRING", State: map[value.DraftOperation]string{value.DraftSave: "PREPARING", value.DraftValidate: "DRAFT", value.DraftPublish: "PUBLISHING", value.DraftDiscard: "DISCARDED"}[kind]}
	work := value.DraftWork{OperationRef: "op_fixture", ClaimantID: "pod_fixture", ClaimGeneration: 1, Kind: kind, Draft: draft,
		Binding: value.SecretDraftBinding{ProjectRef: draft.ProjectRef, SecretRef: draft.SecretRef, DraftRef: draft.Ref,
			DraftGeneration: draft.Generation, ValueType: draft.ValueType, ContentSHA256: hex.EncodeToString(digest[:])},
		StagedNamespace: "kodex-system", StagedName: "draft-fixture", StagedKey: "ciphertext", RuntimeNamespace: "kodex-runtime",
		TargetRevision: 8, LeaseDeadline: now.Add(time.Minute), ExpiresAt: now.Add(time.Hour)}
	encrypted := &encryptedFixture{descriptor: value.DraftEncryptedDescriptor{Namespace: "kodex-system", Name: "draft-fixture", DataKey: "ciphertext", UID: "cipher-uid", ResourceVersion: "42"}}
	if kind != value.DraftSave {
		work.Encrypted = &encrypted.descriptor
	}
	runtime := &runtimeFixture{descriptor: value.DraftMaterialization{Namespace: "kodex-runtime", Name: "secret-fixture", DataKey: "value", UID: "value-uid", ResourceVersion: "84", Revision: 8}}
	result := value.DraftResult{Draft: draft}
	result.Draft.State = map[value.DraftOperation]string{value.DraftSave: "DRAFT", value.DraftValidate: "VALID", value.DraftPublish: "PUBLISHED", value.DraftDiscard: "DISCARDED"}[kind]
	if kind == value.DraftPublish {
		result.Secret = &value.PublishedSecret{Ref: draft.SecretRef, ProjectRef: draft.ProjectRef, Revision: 8}
	}
	owner := &ownerFixture{work: work, result: result, decision: value.DraftRecoveryDecision{EncryptedAction: value.DraftRecoveryKeep, MaterializationAction: value.DraftRecoveryKeep}}
	cipher := &cipherFixture{plaintext: plaintext}
	service, err := New(owner, cipher, encrypted, encrypted, runtime, 1024, "kodex-system", "kodex-runtime")
	if err != nil {
		t.Fatal("fixture service initialization failed")
	}
	return service, owner, encrypted, runtime, cipher
}

func TestDraftCommandsUseExactOwnerWorkAndNeverReturnPlaintext(t *testing.T) {
	for _, kind := range []value.DraftOperation{value.DraftSave, value.DraftValidate, value.DraftPublish, value.DraftDiscard} {
		t.Run(string(kind), func(t *testing.T) {
			service, owner, encrypted, runtime, cipher := serviceFixture(t, kind)
			var input []byte
			if kind == value.DraftSave {
				input = bytes.Clone(cipher.plaintext)
			}
			result, err := service.Execute(t.Context(), kind, "synthetic-grant", input)
			if err != nil || result.Draft.State != owner.result.Draft.State || owner.completed != 1 || owner.failed != 0 {
				t.Fatal("valid draft command did not complete exact intent")
			}
			if len(input) > 0 && !bytes.Equal(input, make([]byte, len(input))) {
				t.Fatal("request plaintext was retained")
			}
			if kind == value.DraftSave && (encrypted.creates != 1 || cipher.encryptions != 1 || runtime.publishes != 0) {
				t.Fatal("save changed runtime publication")
			}
			if kind == value.DraftValidate && (encrypted.reads != 1 || cipher.decryptions != 1 || runtime.publishes != 0) {
				t.Fatal("validate changed runtime publication")
			}
			if kind == value.DraftPublish && (runtime.publishes != 1 || owner.lastMaterialization == nil || owner.lastEncrypted == nil) {
				t.Fatal("publish did not bind both exact effects")
			}
			if kind == value.DraftDiscard && (encrypted.deletes != 1 || cipher.decryptions != 0 || runtime.publishes != 0) {
				t.Fatal("discard did not remain cleanup-only")
			}
		})
	}
}

func TestDraftRejectsForeignExpiredOrUnclaimedOwnerWorkBeforeEffects(t *testing.T) {
	for name, alter := range map[string]func(*value.DraftWork){
		"project":             func(w *value.DraftWork) { w.Binding.ProjectRef = "prj_foreign" },
		"generation":          func(w *value.DraftWork) { w.Binding.DraftGeneration++ },
		"wrong-kind":          func(w *value.DraftWork) { w.Kind = value.DraftDiscard },
		"wrong-state":         func(w *value.DraftWork) { w.Draft.State = "PUBLISHED" },
		"unclaimed":           func(w *value.DraftWork) { w.ClaimantID = ""; w.ClaimGeneration = 0 },
		"expired":             func(w *value.DraftWork) { w.LeaseDeadline = time.Now().Add(-time.Minute) },
		"lease-beyond-expiry": func(w *value.DraftWork) { w.LeaseDeadline = w.ExpiresAt.Add(time.Second) },
		"runtime-namespace":   func(w *value.DraftWork) { w.RuntimeNamespace = "foreign" },
		"staged-namespace":    func(w *value.DraftWork) { w.StagedNamespace = "kodex-runtime" },
	} {
		t.Run(name, func(t *testing.T) {
			service, owner, staged, runtime, cipher := serviceFixture(t, value.DraftSave)
			alter(&owner.work)
			input := bytes.Clone(cipher.plaintext)
			if _, err := service.Execute(t.Context(), value.DraftSave, "synthetic-grant", input); !errors.Is(err, secretdrafts.ErrConflict) {
				t.Fatal("invalid owner work accepted")
			}
			if staged.creates != 0 || runtime.publishes != 0 || owner.completed != 0 || cipher.encryptions != 0 {
				t.Fatal("effect occurred before owner validation")
			}
			if !bytes.Equal(input, make([]byte, len(input))) {
				t.Fatal("failed request retained plaintext")
			}
		})
	}
}

func TestDraftUnknownEffectOrCompletionKeepsRecoveryIntent(t *testing.T) {
	for _, stage := range []string{"storage", "completion", "consume"} {
		t.Run(stage, func(t *testing.T) {
			service, owner, staged, runtime, cipher := serviceFixture(t, value.DraftSave)
			switch stage {
			case "storage":
				staged.err = secretdrafts.ErrUnavailable
			case "completion":
				owner.completeErr = secretdrafts.ErrUnavailable
			case "consume":
				owner.consumeErr = secretdrafts.ErrUnavailable
			}
			input := bytes.Clone(cipher.plaintext)
			if _, err := service.Execute(t.Context(), value.DraftSave, "synthetic-grant", input); err == nil {
				t.Fatal("unknown outcome was reported successful")
			}
			if owner.failed != 0 || staged.creates > 1 || runtime.publishes != 0 {
				t.Fatal("unknown effect was retried or terminalized")
			}
			if !bytes.Equal(input, make([]byte, len(input))) {
				t.Fatal("failed request retained plaintext")
			}
		})
	}
}

func TestDraftInvalidValueFailsBeforeStorage(t *testing.T) {
	for _, kind := range []string{"digest", "utf8", "json"} {
		t.Run(kind, func(t *testing.T) {
			service, owner, staged, _, _ := serviceFixture(t, value.DraftSave)
			input := []byte{0xff}
			if kind != "digest" {
				digest := sha256.Sum256(input)
				owner.work.Binding.ContentSHA256 = hex.EncodeToString(digest[:])
			}
			if kind == "json" {
				owner.work.Binding.ValueType = "JSON"
				owner.work.Draft.ValueType = "JSON"
			}
			if _, err := service.Execute(t.Context(), value.DraftSave, "synthetic-grant", input); err == nil {
				t.Fatal("invalid plaintext was accepted")
			}
			if staged.creates != 0 || owner.failed != 1 {
				t.Fatal("invalid plaintext reached storage or did not close attempt")
			}
		})
	}
}

func TestRecoveryKeepsPublishedEffectAndNeverDecryptsOrRepublishes(t *testing.T) {
	service, owner, staged, runtime, cipher := serviceFixture(t, value.DraftPublish)
	owner.work.LeaseDeadline = time.Now().Add(-time.Minute)
	if err := service.ReconcileOnce(t.Context()); err != nil {
		t.Fatal("owner KEEP reconciliation failed")
	}
	if staged.deletes != 0 || runtime.deletes != 0 || runtime.publishes != 0 || cipher.decryptions != 0 || owner.cleanup != 0 {
		t.Fatal("KEEP or recovery violated effect boundaries")
	}
}

func TestRecoveryRepeatsExactCleanupAcknowledgementAfterNotFound(t *testing.T) {
	service, owner, staged, runtime, _ := serviceFixture(t, value.DraftPublish)
	owner.work.RecoveryEncrypted, owner.work.RecoveryMaterialization = &staged.descriptor, &runtime.descriptor
	owner.decision = value.DraftRecoveryDecision{EncryptedAction: value.DraftRecoveryDelete, MaterializationAction: value.DraftRecoveryDelete}
	owner.cleanupErr = secretdrafts.ErrUnavailable
	if err := service.ReconcileOnce(t.Context()); err == nil {
		t.Fatal("lost cleanup ACK was reported successful")
	}
	owner.cleanupErr = nil
	if err := service.ReconcileOnce(t.Context()); err != nil {
		t.Fatal("NotFound cleanup ACK recovery failed")
	}
	if owner.cleanup != 2 || owner.lastEncrypted == nil || owner.lastMaterialization == nil ||
		*owner.lastEncrypted != staged.descriptor || *owner.lastMaterialization != runtime.descriptor || runtime.publishes != 0 {
		t.Fatal("cleanup lost exact descriptor or republished")
	}
	owner.work.ClaimantID, owner.work.ClaimGeneration = "", 0
	owner.work.Kind = value.DraftSave
	owner.work.RecoveryMaterialization = nil
	if err := service.ReconcileOnce(t.Context()); err != nil {
		t.Fatal("expired PREPARED zero fence was not recovered")
	}
}

func TestRecoveryRejectsReplacementAndUnknownOwnerActionBeforeDelete(t *testing.T) {
	for _, mode := range []string{"replacement", "unknown-action"} {
		t.Run(mode, func(t *testing.T) {
			service, owner, staged, runtime, _ := serviceFixture(t, value.DraftPublish)
			if mode == "replacement" {
				copy := staged.descriptor
				copy.UID = "previous-uid"
				owner.work.RecoveryEncrypted = &copy
			} else {
				owner.decision.EncryptedAction = "UNKNOWN"
			}
			if err := service.ReconcileOnce(t.Context()); !errors.Is(err, secretdrafts.ErrConflict) {
				t.Fatal("unknown recovery boundary accepted")
			}
			if staged.deletes != 0 || runtime.deletes != 0 || owner.cleanup != 0 {
				t.Fatal("unsafe recovery deleted a resource")
			}
		})
	}
}

type recoveryObserverFixture struct{ encrypted, runtime, failures int }

func (observer *recoveryObserverFixture) EncryptedDeleted() { observer.encrypted++ }
func (observer *recoveryObserverFixture) RuntimeDeleted()   { observer.runtime++ }
func (observer *recoveryObserverFixture) RecoveryCompleted(success bool) {
	if !success {
		observer.failures++
	}
}

func TestRecoveryObservesCompletedEffectsWhenCleanupACKFails(t *testing.T) {
	service, owner, _, _, _ := serviceFixture(t, value.DraftPublish)
	observer := &recoveryObserverFixture{}
	service.observer = observer
	owner.decision = value.DraftRecoveryDecision{EncryptedAction: value.DraftRecoveryDelete, MaterializationAction: value.DraftRecoveryDelete}
	owner.cleanupErr = secretdrafts.ErrUnavailable
	if service.ReconcileOnce(t.Context()) == nil {
		t.Fatal("failed ACK reported success")
	}
	if observer.encrypted != 1 || observer.runtime != 1 || observer.failures != 1 {
		t.Fatal("partial outcome lost completed cleanup metrics")
	}
}
