// Package objectstoragetest supplies deterministic in-memory object storage and narrowly
// scoped local E2E input guards; neither provides durable production storage.
// RU: Package objectstoragetest предоставляет детерминированное хранилище в памяти и
// узкие проверки входа локальных E2E; ни то ни другое не является постоянным production-хранилищем.
package objectstoragetest

import (
	"bytes"
	"context"
	"io"
	"sync"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
)

// storedObject pairs a test receipt with an owned body copy so caller buffers cannot mutate stored bytes.
// storedObject связывает тестовую квитанцию с собственной копией тела, чтобы буферы вызывающей стороны не меняли сохранённые байты.
type storedObject struct {
	receipt objectstorage.Receipt
	body    []byte
}

// Store implements the object-storage port with a mutex-protected map and a fixed test version identifier.
// Store реализует порт хранилища объектов через защищённую мьютексом карту и фиксированный тестовый идентификатор версии.
type Store struct {
	mu      sync.RWMutex
	objects map[string]storedObject
}

// New creates an empty isolated store; test cases must share the returned instance explicitly to observe the same objects.
// New создаёт пустое изолированное хранилище; для доступа к общим объектам тесты должны явно использовать один экземпляр.
func New() *Store { return &Store{objects: make(map[string]storedObject)} }

// Check reports availability of the in-memory adapter and rejects a nil receiver without probing any external service.
// Check сообщает о доступности адаптера в памяти и отклоняет пустой экземпляр без обращения к внешнему сервису.
func (store *Store) Check(context.Context) error {
	if store == nil {
		return objectstorage.ErrUnavailable
	}
	return nil
}

// Put validates metadata and exact body length, then atomically replaces the object under its key with a private byte copy.
// Put проверяет метаданные и точную длину тела, затем атомарно заменяет объект по ключу собственной копией байтов.
// It records the supplied digest as a test receipt but does not recompute a content hash or persist bytes outside this process.
// Метод сохраняет переданный дайджест в тестовой квитанции, но не пересчитывает хеш содержимого и не записывает байты за пределами процесса.
func (store *Store) Put(_ context.Context, input objectstorage.PutInput) (objectstorage.Receipt, error) {
	if store == nil || !objectstorage.ValidKey(input.Key) || !objectstorage.ValidDigest(input.Digest) ||
		input.MediaType == "" || input.SizeBytes < 0 || input.Body == nil {
		return objectstorage.Receipt{}, objectstorage.ErrInvalid
	}
	body, err := io.ReadAll(io.LimitReader(input.Body, input.SizeBytes+1))
	if err != nil || int64(len(body)) != input.SizeBytes {
		return objectstorage.Receipt{}, objectstorage.ErrInvalid
	}
	receipt := objectstorage.Receipt{Key: input.Key, VersionID: "memory-v1", ETag: input.Digest, Digest: input.Digest, SizeBytes: input.SizeBytes}
	store.mu.Lock()
	store.objects[input.Key] = storedObject{receipt: receipt, body: append([]byte(nil), body...)}
	store.mu.Unlock()
	return receipt, nil
}

// Get returns the selected object with a fresh reader over copied bytes, leaving the map untouched by reader consumption.
// Get возвращает выбранный объект с новым потоком поверх копии байтов; чтение потока не изменяет содержимое карты.
func (store *Store) Get(_ context.Context, key, versionID string) (objectstorage.Object, error) {
	stored, err := store.read(key, versionID)
	if err != nil {
		return objectstorage.Object{}, err
	}
	return objectstorage.Object{Receipt: stored.receipt, Body: io.NopCloser(bytes.NewReader(stored.body))}, nil
}

// Head returns only the stored receipt after the same key and optional-version checks as Get.
// Head возвращает только сохранённую квитанцию после тех же проверок ключа и необязательной версии, что и Get.
func (store *Store) Head(_ context.Context, key, versionID string) (objectstorage.Receipt, error) {
	stored, err := store.read(key, versionID)
	if err != nil {
		return objectstorage.Receipt{}, err
	}
	return stored.receipt, nil
}

// read selects one key under a read lock, rejects missing or mismatched versions, and copies the body before returning it.
// read выбирает ключ под блокировкой чтения, отклоняет отсутствующую или несовпадающую версию и копирует тело перед возвратом.
func (store *Store) read(key, versionID string) (storedObject, error) {
	if store == nil || !objectstorage.ValidKey(key) {
		return storedObject{}, objectstorage.ErrInvalid
	}
	store.mu.RLock()
	stored, ok := store.objects[key]
	store.mu.RUnlock()
	if !ok || versionID != "" && stored.receipt.VersionID != versionID {
		return storedObject{}, objectstorage.ErrNotFound
	}
	stored.body = append([]byte(nil), stored.body...)
	return stored, nil
}

// Delete removes an existing key under a write lock only when the optional version matches; missing objects remain errors.
// Delete удаляет существующий ключ под блокировкой записи только при совпадении необязательной версии; отсутствие объекта остаётся ошибкой.
func (store *Store) Delete(_ context.Context, key, versionID string) error {
	if store == nil || !objectstorage.ValidKey(key) {
		return objectstorage.ErrInvalid
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	stored, ok := store.objects[key]
	if !ok || versionID != "" && stored.receipt.VersionID != versionID {
		return objectstorage.ErrNotFound
	}
	delete(store.objects, key)
	return nil
}
