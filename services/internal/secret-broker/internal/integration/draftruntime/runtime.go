package draftruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimesecret"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/secretdrafts"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	secretstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
	"k8s.io/apimachinery/pkg/util/validation"
)

// runtimeDataKey fixes the published runtime Secret entry to value, distinct from staging ciphertext keys.
// runtimeDataKey фиксирует published runtime Secret entry как value, отдельно от staging ciphertext keys.
const runtimeDataKey = "value"

// Materializer reuses the existing exact Kubernetes store for effect-bound create/readback/lookup/delete;
// this adapter must not create a second storage authority.
// Materializer использует существующий exact Kubernetes store для effect-bound
// create/readback/lookup/delete; этот adapter не должен создавать вторую storage authority.
type Materializer interface {
	// Namespace returns stable configuration used when constructing and validating the runtime adapter.
	// Namespace возвращает стабильную конфигурацию для создания и проверки runtime adapter.
	Namespace() string
	// CreateImmutableForEffect creates or verifies one immutable exact effect and returns pinned object
	// metadata, not permission to alter another revision.
	// CreateImmutableForEffect создаёт либо сверяет один immutable exact effect и возвращает pinned object
	// metadata, не разрешая изменение иной revision.
	CreateImmutableForEffect(context.Context, secretstore.MaterializationEffect, []byte) (secretstore.Materialization, error)
	// ReadbackExact rereads the exact observed UID/resourceVersion/materialization before publication is
	// accepted.
	// ReadbackExact повторно читает точные observed UID/resourceVersion/materialization до принятия
	// публикации.
	ReadbackExact(context.Context, secretstore.Materialization) (secretstore.Materialization, error)
	// LookupExpectedEffect observes the exact persisted effect without repeating create or returning
	// plaintext.
	// LookupExpectedEffect наблюдает точный persisted effect без повторного create и возврата plaintext.
	LookupExpectedEffect(context.Context, secretstore.MaterializationEffect) (secretstore.Materialization, error)
	// DeleteExact removes only the supplied exact materialization under the underlying store's
	// UID/resourceVersion checks.
	// DeleteExact удаляет лишь переданную exact materialization с UID/resourceVersion checks нижележащего
	// store.
	DeleteExact(context.Context, secretstore.Materialization) error
}

// Store maps validated draft PUBLISH work to the shared materializer's effect identity; it neither
// consumes grants nor decrypts staged values.
// Store отображает проверенную draft PUBLISH work в effect identity общего materializer; grants не
// потребляет и staged values не расшифровывает.
type Store struct {
	materializer Materializer
	namespace    string
}

var _ secretdrafts.RuntimeStore = (*Store)(nil)

// New requires a nonnil materializer and DNS-label namespace, then captures that namespace; callers must
// provide stable configuration, not a changing Namespace result.
// New требует nonnil materializer и DNS-label namespace, затем сохраняет namespace; caller предоставляет
// стабильную конфигурацию, не меняющийся Namespace result.
func New(materializer Materializer) (*Store, error) {
	if materializer == nil || len(validation.IsDNS1123Label(materializer.Namespace())) != 0 {
		return nil, secretdrafts.ErrInvalid
	}
	return &Store{materializer: materializer, namespace: materializer.Namespace()}, nil
}

// Publish validates exact claimed work, cancellation and plaintext bounds/digest, then creates and
// independently reads back a matching immutable effect. Returned descriptors must be equal; it does not
// blindly retry, clear caller plaintext or complete owner lifecycle.
// Publish проверяет exact claimed work, cancellation и plaintext bounds/digest, затем создаёт и независимо
// перечитывает совпавший immutable effect. Returned descriptors должны быть равны; слепого retry, очистки
// caller plaintext и owner lifecycle completion здесь нет.
func (store *Store) Publish(ctx context.Context, work value.DraftWork, plaintext []byte) (value.DraftMaterialization, error) {
	effect, err := store.effect(work)
	if err != nil {
		return value.DraftMaterialization{}, err
	}
	if err := ctx.Err(); err != nil {
		return value.DraftMaterialization{}, err
	}
	digest := sha256.Sum256(plaintext)
	if len(plaintext) == 0 || len(plaintext) > value.MaximumDraftValueBytes || hex.EncodeToString(digest[:]) != effect.ContentSHA256 {
		return value.DraftMaterialization{}, secretdrafts.ErrInvalid
	}
	created, err := store.materializer.CreateImmutableForEffect(ctx, effect, plaintext)
	if err != nil {
		return value.DraftMaterialization{}, mapError(ctx, err)
	}
	if !store.matches(created, effect) {
		return value.DraftMaterialization{}, secretdrafts.ErrConflict
	}
	actual, err := store.materializer.ReadbackExact(ctx, created)
	if err != nil {
		return value.DraftMaterialization{}, mapError(ctx, err)
	}
	if actual != created || !store.matches(actual, effect) {
		return value.DraftMaterialization{}, secretdrafts.ErrConflict
	}
	return cast(actual), nil
}

// Lookup validates exact work/cancellation and requires the observed materialization to match every effect
// pin without replaying publication.
// Lookup проверяет exact work/cancellation и требует совпадения observed materialization со всеми effect
// pins без повторной публикации.
func (store *Store) Lookup(ctx context.Context, work value.DraftWork) (value.DraftMaterialization, error) {
	effect, err := store.effect(work)
	if err != nil {
		return value.DraftMaterialization{}, err
	}
	if err := ctx.Err(); err != nil {
		return value.DraftMaterialization{}, err
	}
	actual, err := store.materializer.LookupExpectedEffect(ctx, effect)
	if err != nil {
		return value.DraftMaterialization{}, mapError(ctx, err)
	}
	if !store.matches(actual, effect) {
		return value.DraftMaterialization{}, secretdrafts.ErrConflict
	}
	return cast(actual), nil
}

// Delete rebuilds the exact effect, rejects a foreign descriptor and delegates one checked deletion after
// cancellation check; owner recovery must already have chosen DELETE.
// Delete восстанавливает exact effect, отклоняет foreign descriptor и делегирует одно проверенное удаление
// после cancellation check; owner recovery должен заранее выбрать DELETE.
func (store *Store) Delete(ctx context.Context, work value.DraftWork, descriptor value.DraftMaterialization) error {
	effect, err := store.effect(work)
	if err != nil {
		return err
	}
	expected := secretstore.Materialization{WorkKind: secretstore.WorkKindDraft, Namespace: descriptor.Namespace, Name: descriptor.Name, Key: descriptor.DataKey, UID: descriptor.UID, ResourceVersion: descriptor.ResourceVersion, Revision: descriptor.Revision, ContentSHA256: descriptor.ContentSHA256, OperationRef: effect.OperationRef, ClaimGeneration: effect.ClaimGeneration, SecretRef: effect.SecretRef}
	if !store.matches(expected, effect) {
		return secretdrafts.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return mapError(ctx, store.materializer.DeleteExact(ctx, expected))
}

// effect accepts only positive claimed PUBLISH work with matching draft/binding/scope and configured
// namespace, then emits WorkKindDraft/value pins. It does not establish grant authority or validate
// lease/state freshness.
// effect принимает лишь positive claimed PUBLISH work с совпавшими draft/binding/scope и настроенным
// namespace, затем создаёт WorkKindDraft/value pins. Grant authority и актуальность lease/state не
// устанавливает.
func (store *Store) effect(work value.DraftWork) (secretstore.MaterializationEffect, error) {
	if work.Kind != value.DraftPublish || work.RuntimeNamespace != store.namespace || work.ClaimGeneration < 1 || !bounded(work.OperationRef, 128) || !bounded(work.ClaimantID, 128) || work.TargetRevision < 1 || work.Binding.Validate() != nil || work.Binding.ScopeKind != work.Draft.ScopeKind || work.Binding.OrganizationRef != work.Draft.OrganizationRef || work.Binding.ProjectRef != work.Draft.ProjectRef || work.Binding.SecretRef != work.Draft.SecretRef || work.Binding.DraftRef != work.Draft.Ref || work.Binding.DraftGeneration != work.Draft.Generation || work.Binding.ValueType != work.Draft.ValueType {
		return secretstore.MaterializationEffect{}, secretdrafts.ErrInvalid
	}
	return secretstore.MaterializationEffect{WorkKind: secretstore.WorkKindDraft, OperationRef: work.OperationRef, ClaimGeneration: work.ClaimGeneration, SecretRef: work.Binding.SecretRef, Key: runtimeDataKey, Revision: work.TargetRevision, ContentSHA256: work.Binding.ContentSHA256}, nil
}

// matches requires canonical versioned name, namespace, work kind, operation/generation,
// secret/key/revision/digest and bounded nonempty UID/RV; it compares metadata, not live object contents.
// matches требует canonical versioned name, namespace, work kind, operation/generation,
// secret/key/revision/digest и bounded непустые UID/RV; сверяет metadata, не содержимое live object.
func (store *Store) matches(actual secretstore.Materialization, effect secretstore.MaterializationEffect) bool {
	name, err := runtimesecret.VersionedKubernetesName(effect.SecretRef, effect.Revision)
	return err == nil && actual.WorkKind == effect.WorkKind && actual.Namespace == store.namespace && actual.Name == name && actual.OperationRef == effect.OperationRef && actual.ClaimGeneration == effect.ClaimGeneration && actual.SecretRef == effect.SecretRef && actual.Key == runtimeDataKey && actual.Revision == effect.Revision && actual.ContentSHA256 == effect.ContentSHA256 && bounded(actual.UID, 128) && bounded(actual.ResourceVersion, 128)
}

// cast copies verified storage pins into the domain descriptor without an additional lookup or authority
// check.
// cast копирует проверенные storage pins в domain descriptor без дополнительного lookup и authority check.
func cast(actual secretstore.Materialization) value.DraftMaterialization {
	return value.DraftMaterialization{Namespace: actual.Namespace, Name: actual.Name, DataKey: actual.Key, UID: actual.UID, ResourceVersion: actual.ResourceVersion, Revision: actual.Revision, ContentSHA256: actual.ContentSHA256}
}

// bounded accepts nonempty byte-bounded text without outer whitespace or NUL/CR/LF/tab; it is not a
// complete resource-reference or Unicode policy.
// bounded принимает непустой byte-bounded text без внешних пробелов и NUL/CR/LF/tab; полной
// resource-reference либо Unicode policy не является.
func bounded(text string, limit int) bool {
	return len(text) > 0 && len(text) <= limit && strings.TrimSpace(text) == text && !strings.ContainsAny(text, "\x00\r\n\t")
}

// mapError preserves nil success, gives current cancellation precedence over an upstream error and maps
// known materialization sentinels; every other diagnostic becomes fixed Unavailable.
// mapError сохраняет nil success, отдаёт текущей cancellation приоритет над upstream error и отображает
// известные materialization sentinels; прочие diagnostics заменяет фиксированным Unavailable.
func mapError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	switch {
	case errors.Is(err, secretstore.ErrMaterializationNotFound):
		return secretdrafts.ErrNotFound
	case errors.Is(err, secretstore.ErrMaterializationConflict):
		return secretdrafts.ErrConflict
	case errors.Is(err, secretstore.ErrMaterializationInvalid), errors.Is(err, secretstore.ErrExactDeletePreconditionsRequired):
		return secretdrafts.ErrInvalid
	default:
		return secretdrafts.ErrUnavailable
	}
}
