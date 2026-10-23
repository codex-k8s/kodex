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

// ownerFixture is a programmable owner stub recording completion/cleanup descriptors and counts; it does
// not enforce grants, claims or durable owner state.
// ownerFixture — настраиваемая owner-подмена, записывающая completion/cleanup descriptors и счётчики;
// grants, claims и устойчивое owner state не проверяет.
type ownerFixture struct {
	work                                value.DraftWork
	result                              value.DraftResult
	consumeErr, completeErr, cleanupErr error
	completed, failed, cleanup          int
	decision                            value.DraftRecoveryDecision
	lastEncrypted                       *value.DraftEncryptedDescriptor
	lastMaterialization                 *value.DraftMaterialization
	cleanupEncrypted                    *value.DraftEncryptedDescriptor
	cleanupMaterialization              *value.DraftMaterialization
}

// Check always reports readiness so command tests exercise service logic rather than owner transport
// health.
// Check всегда сообщает readiness, чтобы command tests проверяли service logic, не здоровье owner
// transport.
func (owner *ownerFixture) Check(context.Context) error { return nil }

// Consume returns configured work/error without inspecting context or grant; successful tests are not
// proof of grant authorization.
// Consume возвращает настроенные work/error без проверки context и grant; успешный тест не доказывает
// grant authorization.
func (owner *ownerFixture) Consume(context.Context, string) (value.DraftWork, error) {
	return owner.work, owner.consumeErr
}

// Complete records one call and the descriptor pointers, then returns the configured result/error without
// committing domain state.
// Complete записывает вызов и descriptor pointers, затем возвращает настроенные result/error без фиксации
// domain state.
func (owner *ownerFixture) Complete(_ context.Context, _ value.DraftWork, encrypted *value.DraftEncryptedDescriptor, materialization *value.DraftMaterialization) (value.DraftResult, error) {
	owner.completed++
	owner.lastEncrypted, owner.lastMaterialization = encrypted, materialization
	return owner.result, owner.completeErr
}

// Fail increments the reported-failure count and succeeds without validating work or persisting a terminal
// transition.
// Fail увеличивает счётчик сообщённых failures и возвращает успех без проверки work и фиксации terminal
// transition.
func (owner *ownerFixture) Fail(context.Context, value.DraftWork) error { owner.failed++; return nil }

// ListRecovery returns the single configured work even if its lease expired, enabling recovery-policy
// tests without a real queue.
// ListRecovery возвращает одну настроенную work даже с истёкшим lease, позволяя проверять recovery policy
// без реальной очереди.
func (owner *ownerFixture) ListRecovery(context.Context) ([]value.DraftWork, error) {
	return []value.DraftWork{owner.work}, nil
}

// Recover records observed descriptor pointers and returns the configured KEEP/DELETE decision without
// owner-side validation.
// Recover записывает observed descriptor pointers и возвращает настроенное KEEP/DELETE decision без
// owner-side validation.
func (owner *ownerFixture) Recover(_ context.Context, _ value.DraftWork, encrypted *value.DraftEncryptedDescriptor, materialization *value.DraftMaterialization) (value.DraftRecoveryDecision, error) {
	owner.lastEncrypted, owner.lastMaterialization = encrypted, materialization
	return owner.decision, nil
}

// CompleteCleanup counts the acknowledgement attempt, records its descriptors and returns the configured
// error; loss can therefore be retried explicitly by tests.
// CompleteCleanup учитывает acknowledgement attempt, записывает descriptors и возвращает настроенную
// ошибку; тест явно повторяет потерянное подтверждение.
func (owner *ownerFixture) CompleteCleanup(_ context.Context, _ value.DraftWork, encrypted *value.DraftEncryptedDescriptor, materialization *value.DraftMaterialization) error {
	owner.cleanup++
	owner.cleanupEncrypted, owner.cleanupMaterialization = encrypted, materialization
	return owner.cleanupErr
}

// cipherFixture counts cryptographic calls and supplies synthetic ciphertext or cloned plaintext; it
// provides no encryption or authenticated-binding proof.
// cipherFixture считает cryptographic calls и предоставляет synthetic ciphertext либо копию plaintext;
// шифрование и authenticated binding не доказывает.
type cipherFixture struct {
	plaintext                []byte
	err                      error
	encryptions, decryptions int
}

// Encrypt counts a seal request and returns fixed synthetic ciphertext/key metadata plus the configured
// error, ignoring the supplied binding and bytes.
// Encrypt учитывает запрос sealing и возвращает фиксированные synthetic ciphertext/key metadata с
// настроенной ошибкой, игнорируя binding и байты.
func (cipher *cipherFixture) Encrypt(context.Context, value.SecretDraftBinding, []byte) (value.EncryptedSecretDraft, error) {
	cipher.encryptions++
	return value.EncryptedSecretDraft{Key: value.DraftEncryptionKey{ID: "fixture", Generation: 1}, Ciphertext: []byte("synthetic ciphertext")}, cipher.err
}

// Decrypt counts an open request and clones configured plaintext even when returning an error, allowing
// service cleanup paths to own that buffer.
// Decrypt учитывает запрос открытия и копирует настроенный plaintext даже вместе с ошибкой, передавая
// владение буфером service cleanup paths.
func (cipher *cipherFixture) Decrypt(context.Context, value.SecretDraftBinding, value.EncryptedSecretDraft) ([]byte, error) {
	cipher.decryptions++
	return bytes.Clone(cipher.plaintext), cipher.err
}

// encryptedFixture models one staging object, call counts and absence with a shared error; it is not
// Kubernetes or an exact-identity verifier.
// encryptedFixture моделирует один staging object, счётчики и отсутствие с общей ошибкой; это не
// Kubernetes и не exact-identity verifier.
type encryptedFixture struct {
	descriptor              value.DraftEncryptedDescriptor
	err                     error
	creates, reads, deletes int
	absent                  bool
}

// Check returns the configured storage error unchanged for dependency-readiness cases.
// Check возвращает настроенную storage error без изменений для dependency-readiness cases.
func (store *encryptedFixture) Check(context.Context) error { return store.err }

// Create counts the attempt and returns a configured descriptor/error without storing bytes or deciding
// whether a write actually committed.
// Create учитывает попытку и возвращает настроенные descriptor/error без хранения байтов и решения о
// фактической фиксации write.
func (store *encryptedFixture) Create(context.Context, value.DraftWork, value.EncryptedSecretDraft) (value.DraftEncryptedDescriptor, error) {
	store.creates++
	return store.descriptor, store.err
}

// Read counts access and returns an empty encrypted value plus the configured error; the cipher stub
// supplies the plaintext independently.
// Read учитывает обращение и возвращает пустое encrypted value с настроенной ошибкой; cipher-подмена
// предоставляет plaintext независимо.
func (store *encryptedFixture) Read(context.Context, value.DraftWork, value.DraftEncryptedDescriptor) (value.EncryptedSecretDraft, error) {
	store.reads++
	return value.EncryptedSecretDraft{}, store.err
}

// Lookup reports NotFound when marked absent, otherwise the configured descriptor/error, without
// incrementing effect counters.
// Lookup сообщает NotFound при признаке absent, иначе настроенные descriptor/error, без увеличения effect
// counters.
func (store *encryptedFixture) Lookup(context.Context, value.DraftWork) (value.DraftEncryptedDescriptor, error) {
	if store.absent {
		return value.DraftEncryptedDescriptor{}, secretdrafts.ErrNotFound
	}
	return store.descriptor, store.err
}

// Delete counts an attempt and marks the object absent even on error, simulating an uncertain completed
// external deletion.
// Delete учитывает попытку и отмечает объект absent даже при ошибке, моделируя неопределённый завершённый
// внешний delete.
func (store *encryptedFixture) Delete(context.Context, value.DraftWork, value.DraftEncryptedDescriptor) error {
	store.deletes++
	store.absent = true
	return store.err
}

// runtimeFixture models publication/deletion observations and counters without holding plaintext or
// implementing real runtime authorization.
// runtimeFixture моделирует publication/deletion observations и счётчики без хранения plaintext и реальной
// runtime authorization.
type runtimeFixture struct {
	descriptor         value.DraftMaterialization
	err                error
	publishes, deletes int
	absent             bool
}

// Publish counts the attempt and returns configured materialization/error without checking input or
// replaying a real Kubernetes operation.
// Publish учитывает попытку и возвращает настроенные materialization/error без проверки input и реальной
// Kubernetes operation.
func (store *runtimeFixture) Publish(context.Context, value.DraftWork, []byte) (value.DraftMaterialization, error) {
	store.publishes++
	return store.descriptor, store.err
}

// Lookup returns NotFound after synthetic deletion, otherwise the configured materialization/error,
// without republishing.
// Lookup возвращает NotFound после synthetic deletion, иначе настроенные materialization/error, без
// повторной публикации.
func (store *runtimeFixture) Lookup(context.Context, value.DraftWork) (value.DraftMaterialization, error) {
	if store.absent {
		return value.DraftMaterialization{}, secretdrafts.ErrNotFound
	}
	return store.descriptor, store.err
}

// Delete counts the attempt and marks synthetic absence before returning the configured error, preserving
// ambiguous-effect test behavior.
// Delete учитывает попытку и отмечает synthetic absence до возврата настроенной ошибки, сохраняя
// моделирование ambiguous effect.
func (store *runtimeFixture) Delete(context.Context, value.DraftWork, value.DraftMaterialization) error {
	store.deletes++
	store.absent = true
	return store.err
}

// serviceFixture builds isolated owner/storage/cipher stubs with matching scoped binding, digest, future
// lease and per-command states; callers can alter them without touching a cluster.
// serviceFixture создаёт изолированные owner/storage/cipher подмены с совпадающими scoped binding, digest,
// будущим lease и состояниями по команде; caller изменяет их без обращения к кластеру.
func serviceFixture(t *testing.T, kind value.DraftOperation) (*Service, *ownerFixture, *encryptedFixture, *runtimeFixture, *cipherFixture) {
	t.Helper()
	plaintext := []byte("synthetic draft value")
	digest := sha256.Sum256(plaintext)
	now := time.Now()
	draft := value.SecretDraft{Ref: "drf_fixture", ScopeKind: "PROJECT", OrganizationRef: "org_fixture", ProjectRef: "prj_fixture", SecretRef: "sec_fixture", Generation: 2, Version: 3,
		ValueType: "STRING", State: map[value.DraftOperation]string{value.DraftSave: "PREPARING", value.DraftValidate: "DRAFT", value.DraftPublish: "PUBLISHING", value.DraftDiscard: "DISCARDED"}[kind]}
	work := value.DraftWork{OperationRef: "op_fixture", ClaimantID: "pod_fixture", ClaimGeneration: 1, Kind: kind, Draft: draft,
		Binding: value.SecretDraftBinding{ScopeKind: draft.ScopeKind, OrganizationRef: draft.OrganizationRef, ProjectRef: draft.ProjectRef, SecretRef: draft.SecretRef, DraftRef: draft.Ref,
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
		result.Secret = &value.PublishedSecret{Ref: draft.SecretRef, ScopeKind: draft.ScopeKind, OrganizationRef: draft.OrganizationRef, ProjectRef: draft.ProjectRef, Revision: 8}
	}
	owner := &ownerFixture{work: work, result: result, decision: value.DraftRecoveryDecision{EncryptedAction: value.DraftRecoveryKeep, MaterializationAction: value.DraftRecoveryKeep}}
	cipher := &cipherFixture{plaintext: plaintext}
	service, err := New(owner, cipher, encrypted, encrypted, runtime, 1024, "kodex-system", "kodex-runtime")
	if err != nil {
		t.Fatal("fixture service initialization failed")
	}
	return service, owner, encrypted, runtime, cipher
}

// TestDraftCommandsUseExactOwnerWorkAndNeverReturnPlaintext checks each command's completion/state,
// relevant storage/crypto call counts and SAVE input clearing; it uses synthetic ports rather than real
// encryption or grant validation.
// TestDraftCommandsUseExactOwnerWorkAndNeverReturnPlaintext проверяет completion/state каждой команды,
// соответствующие storage/crypto счётчики и очистку SAVE input; synthetic ports не доказывают реальное
// шифрование и grant validation.
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

// TestDraftRejectsForeignExpiredOrUnclaimedOwnerWorkBeforeEffects mutates one
// scope/kind/state/claim/time/namespace coordinate, requiring conflict and no
// create/publish/complete/encrypt while clearing input.
// TestDraftRejectsForeignExpiredOrUnclaimedOwnerWorkBeforeEffects меняет одну
// scope/kind/state/claim/time/namespace координату, требуя conflict, отсутствия
// create/publish/complete/encrypt и очистки input.
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

// TestDraftUnknownEffectOrCompletionKeepsRecoveryIntent injects consume/storage/completion failure and
// checks error, input clearing, no Fail, no repeated create and no runtime publication.
// TestDraftUnknownEffectOrCompletionKeepsRecoveryIntent внедряет consume/storage/completion failure и
// проверяет ошибку, очистку input, отсутствие Fail, повторного create и runtime publication.
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

// TestDraftInvalidValueFailsBeforeStorage covers digest mismatch and matching-digest invalid UTF-8/JSON,
// requiring one owner Fail and no staging Create.
// TestDraftInvalidValueFailsBeforeStorage проверяет digest mismatch и некорректные UTF-8/JSON с верным
// digest, требуя одного owner Fail и отсутствия staging Create.
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

// TestRecoveryKeepsPublishedEffectAndNeverDecryptsOrRepublishes uses an expired PUBLISH lease with KEEP
// decisions and checks no deletion, publication, decryption or cleanup acknowledgement.
// TestRecoveryKeepsPublishedEffectAndNeverDecryptsOrRepublishes использует истёкший PUBLISH lease с KEEP
// decisions и проверяет отсутствие deletion, publication, decryption и cleanup acknowledgement.
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

// TestRecoveryAcknowledgesOnlyDeletedCiphertextWhenPublicationIsRetained requires exact ciphertext
// cleanup/ack while runtime KEEP excludes its descriptor and deletion.
// TestRecoveryAcknowledgesOnlyDeletedCiphertextWhenPublicationIsRetained требует exact ciphertext
// cleanup/ack, тогда как runtime KEEP исключает его descriptor и удаление.
func TestRecoveryAcknowledgesOnlyDeletedCiphertextWhenPublicationIsRetained(t *testing.T) {
	service, owner, staged, runtime, _ := serviceFixture(t, value.DraftPublish)
	owner.work.RecoveryEncrypted = &staged.descriptor
	owner.decision = value.DraftRecoveryDecision{EncryptedAction: value.DraftRecoveryDelete, MaterializationAction: value.DraftRecoveryKeep}
	if err := service.ReconcileOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if owner.cleanup != 1 || owner.cleanupEncrypted == nil || *owner.cleanupEncrypted != staged.descriptor || owner.cleanupMaterialization != nil || staged.deletes != 1 || runtime.deletes != 0 {
		t.Fatal("cleanup ACK included retained publication or lost ciphertext deletion")
	}
}

// TestRecoveryRepeatsExactCleanupAcknowledgementAfterNotFound loses the first ACK, then verifies exact
// descriptors survive absence and no republication; it also exercises zero-claim SAVE recovery.
// TestRecoveryRepeatsExactCleanupAcknowledgementAfterNotFound теряет первый ACK, затем проверяет
// сохранение exact descriptors после отсутствия объектов и отсутствие повторной публикации; дополнительно
// проверяет zero-claim SAVE recovery.
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

// TestRecoveryRejectsReplacementAndUnknownOwnerActionBeforeDelete changes an expected UID or injects an
// unknown action, requiring conflict before either deletion or cleanup acknowledgement.
// TestRecoveryRejectsReplacementAndUnknownOwnerActionBeforeDelete меняет ожидаемый UID либо внедряет
// unknown action, требуя conflict до обоих удалений и cleanup acknowledgement.
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

// recoveryObserverFixture counts completed local deletions and failed reconciliation results
// independently, without representing owner receipts.
// recoveryObserverFixture независимо считает выполненные local deletions и неуспешные reconciliation
// results, не моделируя owner receipts.
type recoveryObserverFixture struct{ encrypted, runtime, failures int }

// EncryptedDeleted increments only the synthetic successful-ciphertext-deletion counter.
// EncryptedDeleted увеличивает лишь synthetic счётчик успешных ciphertext deletions.
func (observer *recoveryObserverFixture) EncryptedDeleted() { observer.encrypted++ }

// RuntimeDeleted increments only the synthetic successful-runtime-deletion counter.
// RuntimeDeleted увеличивает лишь synthetic счётчик успешных runtime deletions.
func (observer *recoveryObserverFixture) RuntimeDeleted() { observer.runtime++ }

// RecoveryCompleted counts failure results only and leaves successful deletion counters intact.
// RecoveryCompleted учитывает только failure results и не изменяет счётчики успешных удалений.
func (observer *recoveryObserverFixture) RecoveryCompleted(success bool) {
	if !success {
		observer.failures++
	}
}

// TestRecoveryObservesCompletedEffectsWhenCleanupACKFails makes both deletes succeed and the owner ACK
// fail, requiring one recorded effect of each kind and one overall recovery failure.
// TestRecoveryObservesCompletedEffectsWhenCleanupACKFails делает оба delete успешными, а owner ACK
// неуспешным, требуя учёта каждого эффекта и одного общего recovery failure.
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
