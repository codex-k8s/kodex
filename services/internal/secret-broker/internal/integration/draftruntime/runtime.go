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

const runtimeDataKey = "value"

// Materializer реализуется существующим kubernetes.Store с exact UID/RV
// readback и delete preconditions. Adapter не создаёт второй Kubernetes store.
type Materializer interface {
	Namespace() string
	CreateImmutableForEffect(context.Context, secretstore.MaterializationEffect, []byte) (secretstore.Materialization, error)
	ReadbackExact(context.Context, secretstore.Materialization) (secretstore.Materialization, error)
	LookupExpectedEffect(context.Context, secretstore.MaterializationEffect) (secretstore.Materialization, error)
	DeleteExact(context.Context, secretstore.Materialization) error
}

type Store struct {
	materializer Materializer
	namespace    string
}

var _ secretdrafts.RuntimeStore = (*Store)(nil)

func New(materializer Materializer) (*Store, error) {
	if materializer == nil || len(validation.IsDNS1123Label(materializer.Namespace())) != 0 {
		return nil, secretdrafts.ErrInvalid
	}
	return &Store{materializer: materializer, namespace: materializer.Namespace()}, nil
}

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

func (store *Store) Delete(ctx context.Context, work value.DraftWork, descriptor value.DraftMaterialization) error {
	effect, err := store.effect(work)
	if err != nil {
		return err
	}
	expected := secretstore.Materialization{Namespace: descriptor.Namespace, Name: descriptor.Name, Key: descriptor.DataKey, UID: descriptor.UID, ResourceVersion: descriptor.ResourceVersion, Revision: descriptor.Revision, ContentSHA256: descriptor.ContentSHA256, OperationRef: effect.OperationRef, ClaimGeneration: effect.ClaimGeneration, SecretRef: effect.SecretRef}
	if !store.matches(expected, effect) {
		return secretdrafts.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return mapError(ctx, store.materializer.DeleteExact(ctx, expected))
}

func (store *Store) effect(work value.DraftWork) (secretstore.MaterializationEffect, error) {
	if work.Kind != value.DraftPublish || work.RuntimeNamespace != store.namespace || work.ClaimGeneration < 1 || !bounded(work.OperationRef, 128) || !bounded(work.ClaimantID, 128) || work.TargetRevision < 1 || work.Binding.Validate() != nil || work.Binding.ProjectRef != work.Draft.ProjectRef || work.Binding.SecretRef != work.Draft.SecretRef || work.Binding.DraftRef != work.Draft.Ref || work.Binding.DraftGeneration != work.Draft.Generation || work.Binding.ValueType != work.Draft.ValueType {
		return secretstore.MaterializationEffect{}, secretdrafts.ErrInvalid
	}
	return secretstore.MaterializationEffect{OperationRef: work.OperationRef, ClaimGeneration: work.ClaimGeneration, SecretRef: work.Binding.SecretRef, Key: runtimeDataKey, Revision: work.TargetRevision, ContentSHA256: work.Binding.ContentSHA256}, nil
}

func (store *Store) matches(actual secretstore.Materialization, effect secretstore.MaterializationEffect) bool {
	name, err := runtimesecret.VersionedKubernetesName(effect.SecretRef, effect.Revision)
	return err == nil && actual.Namespace == store.namespace && actual.Name == name && actual.OperationRef == effect.OperationRef && actual.ClaimGeneration == effect.ClaimGeneration && actual.SecretRef == effect.SecretRef && actual.Key == runtimeDataKey && actual.Revision == effect.Revision && actual.ContentSHA256 == effect.ContentSHA256 && bounded(actual.UID, 128) && bounded(actual.ResourceVersion, 128)
}

func cast(actual secretstore.Materialization) value.DraftMaterialization {
	return value.DraftMaterialization{Namespace: actual.Namespace, Name: actual.Name, DataKey: actual.Key, UID: actual.UID, ResourceVersion: actual.ResourceVersion, Revision: actual.Revision, ContentSHA256: actual.ContentSHA256}
}
func bounded(text string, limit int) bool {
	return len(text) > 0 && len(text) <= limit && strings.TrimSpace(text) == text && !strings.ContainsAny(text, "\x00\r\n\t")
}
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
