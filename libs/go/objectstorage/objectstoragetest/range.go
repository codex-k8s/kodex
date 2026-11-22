package objectstoragetest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
)

// ReadRange returns an owned exact slice only while all immutable source pins still match.
// The fixture verifies slice bytes but makes no durability or actor-authorization claim.
// ReadRange возвращает owned exact slice лишь при совпадении всех immutable source pins.
// Fixture проверяет slice bytes, но не доказывает durability либо actor authorization.
func (store *Store) ReadRange(ctx context.Context, input objectstorage.RangeRead) ([]byte, error) {
	if ctx == nil || input.Validate() != nil {
		return nil, objectstorage.ErrInvalid
	}
	if store == nil || ctx.Err() != nil {
		return nil, objectstorage.ErrUnavailable
	}
	store.mu.RLock()
	object, ok := store.objects[input.Object.Key]
	if !ok {
		store.mu.RUnlock()
		return nil, objectstorage.ErrNotFound
	}
	if object.receipt != input.Object || int64(len(object.body)) != input.Object.SizeBytes {
		store.mu.RUnlock()
		return nil, objectstorage.ErrConflict
	}
	payload := bytes.Clone(object.body[input.Offset : input.Offset+input.Length])
	store.mu.RUnlock()
	sum := sha256.Sum256(payload)
	if hex.EncodeToString(sum[:]) != strings.TrimPrefix(input.Digest, "sha256:") || ctx.Err() != nil {
		clear(payload)
		return nil, objectstorage.ErrConflict
	}
	return payload, nil
}

// ReadRange selects one exact retained version under the fixture lock and copies only the bounded slice.
// A newer object at the same key never changes an admitted source; this proves no remote durability.
// ReadRange выбирает exact retained version под fixture lock и копирует лишь bounded slice.
// Новый object того же key не меняет admitted source; remote durability не доказывается.
func (store *VersionedStore) ReadRange(ctx context.Context, input objectstorage.RangeRead) ([]byte, error) {
	if ctx == nil || input.Validate() != nil {
		return nil, objectstorage.ErrInvalid
	}
	if store == nil || ctx.Err() != nil {
		return nil, objectstorage.ErrUnavailable
	}
	store.mu.RLock()
	var payload []byte
	found := false
	for _, object := range store.objects[input.Object.Key] {
		if object.receipt.VersionID != input.Object.VersionID {
			continue
		}
		found = true
		if object.receipt != input.Object || int64(len(object.body)) != input.Object.SizeBytes {
			store.mu.RUnlock()
			return nil, objectstorage.ErrConflict
		}
		payload = bytes.Clone(object.body[input.Offset : input.Offset+input.Length])
		break
	}
	store.mu.RUnlock()
	if !found {
		return nil, objectstorage.ErrNotFound
	}
	sum := sha256.Sum256(payload)
	if hex.EncodeToString(sum[:]) != strings.TrimPrefix(input.Digest, "sha256:") || ctx.Err() != nil {
		clear(payload)
		return nil, objectstorage.ErrConflict
	}
	return payload, nil
}
