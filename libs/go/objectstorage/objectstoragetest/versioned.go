package objectstoragetest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
)

// VersionedStore keeps immutable local object versions for receipt/recovery tests. It proves no
// remote durability, S3 policy or production readiness; New retains its existing fixture semantics.
// VersionedStore хранит immutable local object versions для receipt/recovery tests. Remote
// durability, S3 policy и production readiness не доказывает; поведение New не меняется.
type VersionedStore struct {
	mu       sync.RWMutex
	sequence uint64
	objects  map[string][]storedObject
}

// NewVersioned creates an isolated opt-in version history whose bodies are owned by the fixture.
// NewVersioned создаёт isolated opt-in version history с собственными копиями bytes.
func NewVersioned() *VersionedStore {
	return &VersionedStore{objects: make(map[string][]storedObject)}
}

// Check reports fixture availability without contacting a provider or mutating object history.
// Check проверяет доступность fixture без provider calls либо изменения object history.
func (store *VersionedStore) Check(context.Context) error {
	if store == nil {
		return objectstorage.ErrUnavailable
	}
	return nil
}

// CheckVersioning confirms this fixture's immutable version map, not any deployed bucket policy.
// CheckVersioning подтверждает immutable version map fixture, не deployed bucket policy.
func (store *VersionedStore) CheckVersioning(ctx context.Context) error { return store.Check(ctx) }

// Put validates exact body length/hash and assigns a fresh version under the mutex. Caller-owned
// buffers may subsequently be cleared; no existing version can be replaced by another PUT.
// Put проверяет exact body length/hash и назначает fresh version под mutex. Caller может очистить
// исходные buffers; никакая существующая version не заменяется новым PUT.
func (store *VersionedStore) Put(ctx context.Context, input objectstorage.PutInput) (objectstorage.Receipt, error) {
	if store == nil || !objectstorage.ValidKey(input.Key) || !objectstorage.ValidDigest(input.Digest) || input.MediaType == "" || input.SizeBytes < 0 || input.SizeBytes == math.MaxInt64 || input.Body == nil {
		return objectstorage.Receipt{}, objectstorage.ErrInvalid
	}
	body, err := io.ReadAll(io.LimitReader(input.Body, input.SizeBytes+1))
	digest := sha256.Sum256(body)
	if err != nil || int64(len(body)) != input.SizeBytes || input.Digest != "sha256:"+hex.EncodeToString(digest[:]) || ctx.Err() != nil {
		clear(body)
		return objectstorage.Receipt{}, objectstorage.ErrInvalid
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.sequence == ^uint64(0) {
		clear(body)
		return objectstorage.Receipt{}, objectstorage.ErrUnavailable
	}
	store.sequence++
	receipt := objectstorage.Receipt{Key: input.Key, VersionID: "memory-version-" + strconv.FormatUint(store.sequence, 10), ETag: input.Digest, Digest: input.Digest, SizeBytes: input.SizeBytes}
	store.objects[input.Key] = append(store.objects[input.Key], storedObject{receipt: receipt, body: body})
	return receipt, nil
}

// read selects an explicit immutable version or the latest retained version and returns owned bytes.
// read выбирает explicit immutable version либо latest retained version и возвращает owned bytes.
func (store *VersionedStore) read(key, version string) (storedObject, error) {
	if store == nil || !objectstorage.ValidKey(key) {
		return storedObject{}, objectstorage.ErrInvalid
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	versions := store.objects[key]
	for index := len(versions) - 1; index >= 0; index-- {
		selected := versions[index]
		if version == "" || selected.receipt.VersionID == version {
			selected.body = append([]byte(nil), selected.body...)
			return selected, nil
		}
	}
	return storedObject{}, objectstorage.ErrNotFound
}

// Get returns a private body copy of the exact selected version, never a fallback after mismatch.
// Get возвращает private body copy exact selected version, без fallback при mismatch.
func (store *VersionedStore) Get(_ context.Context, key, version string) (objectstorage.Object, error) {
	selected, err := store.read(key, version)
	if err != nil {
		return objectstorage.Object{}, err
	}
	return objectstorage.Object{Receipt: selected.receipt, Body: io.NopCloser(bytes.NewReader(selected.body))}, nil
}

// Head checks the same explicit-version selection without returning any body bytes.
// Head проверяет тот же explicit-version selection без выдачи body bytes.
func (store *VersionedStore) Head(_ context.Context, key, version string) (objectstorage.Receipt, error) {
	selected, err := store.read(key, version)
	clear(selected.body)
	return selected.receipt, err
}

// Delete removes only the selected version and preserves every sibling version. Empty version
// selects the latest retained object; absence is an error, never positive deletion evidence.
// Delete удаляет только selected version, сохраняя sibling versions. Empty version выбирает
// latest retained object; отсутствие — ошибка, не positive deletion evidence.
func (store *VersionedStore) Delete(_ context.Context, key, version string) error {
	if store == nil || !objectstorage.ValidKey(key) {
		return objectstorage.ErrInvalid
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	versions := store.objects[key]
	for index := len(versions) - 1; index >= 0; index-- {
		if version == "" || versions[index].receipt.VersionID == version {
			clear(versions[index].body)
			copy(versions[index:], versions[index+1:])
			versions[len(versions)-1] = storedObject{}
			store.objects[key] = versions[:len(versions)-1]
			return nil
		}
	}
	return objectstorage.ErrNotFound
}

// CheckDeleteOnce accepts the in-memory one-attempt implementation without external IO.
// CheckDeleteOnce подтверждает in-memory one-attempt implementation без external IO.
func (store *VersionedStore) CheckDeleteOnce(ctx context.Context) error {
	if ctx.Err() != nil {
		return objectstorage.ErrUnavailable
	}
	return nil
}

// DeleteOnce models one exact-version attempt without retries or selecting latest. The test
// adapter checks cancellation before the existing locked deletion; uncertainty belongs to callers.
// DeleteOnce моделирует один exact-version attempt без retries либо выбора latest. Test adapter
// проверяет отмену перед прежним locked deletion; unknown outcome принадлежит callers.
func (store *VersionedStore) DeleteOnce(ctx context.Context, key, version string) error {
	if version == "" || len(version) > 1024 || strings.EqualFold(version, "null") || strings.ContainsAny(version, "\r\n\x00") {
		return objectstorage.ErrInvalid
	}
	if ctx.Err() != nil {
		return objectstorage.ErrUnavailable
	}
	return store.Delete(ctx, key, version)
}

// VerifyVersionAbsent proves exact absence under the fixture's own complete immutable-version map.
// It qualifies only this local disposable store, never a provider HTTP response or deployment.
// VerifyVersionAbsent доказывает exact absence под lock полной immutable-version map fixture.
// Квалифицирует лишь локальное disposable store, не provider HTTP response либо deployment.
func (store *VersionedStore) VerifyVersionAbsent(ctx context.Context, key, version string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if store == nil || !objectstorage.ValidKey(key) || version == "" || strings.EqualFold(version, "null") {
		return objectstorage.ErrInvalid
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	for _, item := range store.objects[key] {
		if item.receipt.VersionID == version {
			return objectstorage.ErrConflict
		}
	}
	return ctx.Err()
}
