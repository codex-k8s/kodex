// Package objectstoragetest предоставляет детерминированный in-memory adapter для component tests.
package objectstoragetest

import (
	"bytes"
	"context"
	"io"
	"sync"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
)

type storedObject struct {
	receipt objectstorage.Receipt
	body    []byte
}

type Store struct {
	mu      sync.RWMutex
	objects map[string]storedObject
}

func New() *Store { return &Store{objects: make(map[string]storedObject)} }

func (store *Store) Check(context.Context) error {
	if store == nil {
		return objectstorage.ErrUnavailable
	}
	return nil
}

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

func (store *Store) Get(_ context.Context, key, versionID string) (objectstorage.Object, error) {
	stored, err := store.read(key, versionID)
	if err != nil {
		return objectstorage.Object{}, err
	}
	return objectstorage.Object{Receipt: stored.receipt, Body: io.NopCloser(bytes.NewReader(stored.body))}, nil
}

func (store *Store) Head(_ context.Context, key, versionID string) (objectstorage.Receipt, error) {
	stored, err := store.read(key, versionID)
	if err != nil {
		return objectstorage.Receipt{}, err
	}
	return stored.receipt, nil
}

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
