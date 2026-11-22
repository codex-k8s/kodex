package objectstoragetest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
)

// TestReadRangePinsVersionAndOwnsBytes checks fixture parity without claiming remote durability.
// TestReadRangePinsVersionAndOwnsBytes проверяет fixture parity без remote durability claim.
func TestReadRangePinsVersionAndOwnsBytes(t *testing.T) {
	store := New()
	body := []byte("0123456789")
	sum := sha256.Sum256(body)
	receipt, err := store.Put(t.Context(), objectstorage.PutInput{Key: "organizations/org/source/private", MediaType: "application/octet-stream", SizeBytes: 10, Digest: "sha256:" + hex.EncodeToString(sum[:]), Body: bytes.NewReader(body)})
	if err != nil {
		t.Fatal("prepare immutable range fixture")
	}
	slice := sha256.Sum256([]byte("345"))
	request := objectstorage.RangeRead{Object: receipt, Offset: 3, Length: 3, Digest: "sha256:" + hex.EncodeToString(slice[:])}
	got, err := store.ReadRange(t.Context(), request)
	if err != nil || string(got) != "345" {
		t.Fatal("range fixture failed")
	}
	got[0] = 'x'
	clear(got)
	again, err := store.ReadRange(t.Context(), request)
	if err != nil || string(again) != "345" {
		t.Fatal("range buffer was not owned")
	}
	clear(again)
	wrong := request
	wrong.Object.VersionID = "foreign-version"
	if got, err = store.ReadRange(t.Context(), wrong); !errors.Is(err, objectstorage.ErrConflict) || got != nil {
		t.Fatal("foreign version admitted")
	}
	wrong = request
	wrong.Digest = receipt.Digest
	if got, err = store.ReadRange(t.Context(), wrong); !errors.Is(err, objectstorage.ErrConflict) || got != nil {
		t.Fatal("corrupt slice commitment admitted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got, err = store.ReadRange(ctx, request); !errors.Is(err, objectstorage.ErrUnavailable) || got != nil {
		t.Fatal("expired range admitted")
	}
}

// TestVersionedRangeRetainsOriginalSource proves that moving latest cannot substitute a pinned range.
// It covers owned slices and unknown versions without reading or copying the complete fixture body.
// TestVersionedRangeRetainsOriginalSource доказывает, что смена latest не подменяет pinned range.
// Проверяет owned slices и unknown versions без чтения либо копирования полного fixture body.
func TestVersionedRangeRetainsOriginalSource(t *testing.T) {
	store := NewVersioned()
	var original objectstorage.Receipt
	for index, body := range []string{"original immutable content", "replacement latest content"} {
		sum := sha256.Sum256([]byte(body))
		receipt, err := store.Put(t.Context(), objectstorage.PutInput{Key: "organizations/org/source/private", MediaType: "application/octet-stream", SizeBytes: int64(len(body)), Digest: "sha256:" + hex.EncodeToString(sum[:]), Body: bytes.NewBufferString(body)})
		if err != nil {
			t.Fatal("prepare versioned source")
		}
		if index == 0 {
			original = receipt
		}
	}
	sum := sha256.Sum256([]byte("immutable"))
	request := objectstorage.RangeRead{Object: original, Offset: 9, Length: 9, Digest: "sha256:" + hex.EncodeToString(sum[:])}
	for range 2 {
		body, err := store.ReadRange(t.Context(), request)
		if err != nil || string(body) != "immutable" {
			t.Fatal("latest replaced original source")
		}
		clear(body)
	}
	request.Object.VersionID = "unknown-version"
	if body, err := store.ReadRange(t.Context(), request); !errors.Is(err, objectstorage.ErrNotFound) || body != nil {
		t.Fatal("unknown source fell back to latest")
	}
}
