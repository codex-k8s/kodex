package platform

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// artifactDeleteReadbackStore records exact context/key/version Delete and Head calls with
// configured synthetic outcomes. Other object-store methods remain a nil embedded port so
// unexpected operations cannot silently succeed; no storage or database is contacted.
// artifactDeleteReadbackStore учитывает Delete и Head с точными context, key и version и
// синтетическими исходами. Остальные методы — nil embedded port, чтобы неожиданные операции не
// могли незаметно пройти; storage и БД не вызываются.
type artifactDeleteReadbackStore struct {
	objectstorage.Store
	t                      *testing.T
	ctx                    context.Context
	key, version           string
	deleteError, headError error
	calls                  []string
}

// Delete checks the original selector/context, records one delete and returns the configured
// synthetic failure. It changes no object state and provides no retry or cleanup acknowledgment.
// Delete проверяет исходные selector и context, учитывает delete и возвращает синтетическую
// ошибку. Состояние объекта не меняет, повтор и cleanup ACK не создаёт.
func (store *artifactDeleteReadbackStore) Delete(ctx context.Context, key, version string) error {
	store.t.Helper()
	if ctx != store.ctx || key != store.key || version != store.version {
		store.t.Fatal("object delete changed context, key or version")
	}
	store.calls = append(store.calls, "delete")
	return store.deleteError
}

// Head checks the same original selector/context, records readback and returns empty metadata plus
// the configured error. Nil error intentionally represents an existing object even with empty
// receipt fields, preserving the original absence predicate.
// Head проверяет те же selector и context, учитывает readback и возвращает пустую metadata с
// заданной ошибкой. Nil error намеренно означает существующий объект даже с пустой receipt,
// сохраняя прежний критерий отсутствия.
func (store *artifactDeleteReadbackStore) Head(ctx context.Context, key, version string) (objectstorage.Receipt, error) {
	store.t.Helper()
	if ctx != store.ctx || key != store.key || version != store.version {
		store.t.Fatal("object readback changed context, key or version")
	}
	store.calls = append(store.calls, "head")
	return objectstorage.Receipt{}, store.headError
}

// TestArtifactDeleteRequiresAuthoritativeAbsence checks thirteen synthetic delete/readback
// outcomes with exact call order, context/key/version, no retry and no storage-detail disclosure.
// It covers NotFound/wrapped, persistent objects, invalid/conflict/unknown/canceled failures and
// the existing empty-version selector; nil repository pool ensures this primitive cannot ACK owner
// state. This does not prove live storage or PostgreSQL behavior.
// TestArtifactDeleteRequiresAuthoritativeAbsence проверяет тринадцать синтетических delete и
// readback исходов с точными порядком, context, key и version, отсутствием повтора и storage
// detail. Покрывает NotFound и wrapped, оставшийся объект, invalid, conflict, unknown, canceled и
// прежний пустой version selector; nil repository pool исключает owner ACK этим примитивом. Не
// является живой приёмкой storage и PostgreSQL.
func TestArtifactDeleteRequiresAuthoritativeAbsence(t *testing.T) {
	for _, test := range []struct {
		name                   string
		deleteError, headError error
		want                   error
		readback, emptyVersion bool
	}{
		{"deleted", nil, objectstorage.ErrNotFound, nil, true, false},
		{"already absent", objectstorage.ErrNotFound, objectstorage.ErrNotFound, nil, true, false},
		{"wrapped absence", fmt.Errorf("fixture: %w", objectstorage.ErrNotFound), fmt.Errorf("fixture: %w", objectstorage.ErrNotFound), nil, true, false},
		{"delete unknown", errors.New("PRIVATE_OBJECT_STORAGE_DETAIL"), nil, errs.ErrUnavailable, false, false},
		{"delete invalid", objectstorage.ErrInvalid, nil, errs.ErrInvalid, false, false},
		{"delete conflict", objectstorage.ErrConflict, nil, errs.ErrConflict, false, false},
		{"still present", nil, nil, errs.ErrConflict, true, false},
		{"readback unknown", nil, errors.New("PRIVATE_OBJECT_STORAGE_DETAIL"), errs.ErrUnavailable, true, false},
		{"readback invalid", nil, objectstorage.ErrInvalid, errs.ErrInvalid, true, false},
		{"readback conflict", nil, objectstorage.ErrConflict, errs.ErrConflict, true, false},
		{"delete canceled", context.Canceled, nil, errs.ErrUnavailable, false, false},
		{"readback canceled", nil, context.Canceled, errs.ErrUnavailable, true, false},
		{"empty version selector", nil, objectstorage.ErrNotFound, nil, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			version := "object-version"
			if test.emptyVersion {
				version = ""
			}
			store := &artifactDeleteReadbackStore{t: t, ctx: t.Context(), key: "object-key", version: version, deleteError: test.deleteError, headError: test.headError}
			err := (&Repository{objects: store}).deleteArtifactObjectAndVerify(store.ctx, store.key, version)
			if !errors.Is(err, test.want) {
				t.Fatal("object cleanup changed its error mapping")
			}
			wantCalls := []string{"delete"}
			if test.readback {
				wantCalls = append(wantCalls, "head")
			}
			if !reflect.DeepEqual(store.calls, wantCalls) {
				t.Fatal("object cleanup changed call order or retried a storage operation")
			}
			if err != nil && strings.Contains(err.Error(), "PRIVATE_OBJECT_STORAGE_DETAIL") {
				t.Fatal("object cleanup disclosed a storage diagnostic")
			}
		})
	}
}
