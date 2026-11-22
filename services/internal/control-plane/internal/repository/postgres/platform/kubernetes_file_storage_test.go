package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
)

// fileOriginalStoreFixture observes exact-version requests and a deliberately partial response body.
// fileOriginalStoreFixture наблюдает exact-version requests и намеренно partial response body.
type fileOriginalStoreFixture struct {
	objectstorage.Store
	object objectstorage.Object
	err    error
	calls  int
}

// Get transfers one fixture body without retrying a failed provider operation.
// Get передаёт fixture body без повторного failed provider operation.
func (f *fileOriginalStoreFixture) Get(context.Context, string, string) (objectstorage.Object, error) {
	f.calls++
	return f.object, f.err
}

// fileOriginalBodyFixture records positive body closure, including close failures.
// fileOriginalBodyFixture фиксирует positive body closure, включая close failures.
type fileOriginalBodyFixture struct {
	io.Reader
	closed   bool
	closeErr error
}

// Close records body ownership release without making the simulated provider healthy.
// Close фиксирует body ownership release, не делает simulated provider healthy.
func (b *fileOriginalBodyFixture) Close() error { b.closed = true; return b.closeErr }

// TestKubernetesFileOriginalPreservesOldVersion verifies the actual owner byte-reader against a retained older fixture version.
// It proves no artifact ACL, PostgreSQL origin pin or S3 durability; those require the owner component contour.
// TestKubernetesFileOriginalPreservesOldVersion проверяет actual owner byte-reader по retained older fixture version.
// Artifact ACL, PostgreSQL origin pin либо S3 durability не доказывает: для них нужен owner component contour.
func TestKubernetesFileOriginalPreservesOldVersion(t *testing.T) {
	store := objectstoragetest.NewVersioned()
	first := []byte("original synthetic bytes")
	sum := sha256.Sum256(first)
	receipt, err := store.Put(t.Context(), objectstorage.PutInput{Key: "fixture/file-original", MediaType: "application/octet-stream", Digest: "sha256:" + hex.EncodeToString(sum[:]), SizeBytes: int64(len(first)), Body: bytes.NewReader(first)})
	if err != nil {
		t.Fatal(err)
	}
	newer := []byte("later head")
	next := sha256.Sum256(newer)
	if _, err := store.Put(t.Context(), objectstorage.PutInput{Key: receipt.Key, MediaType: "application/octet-stream", Digest: "sha256:" + hex.EncodeToString(next[:]), SizeBytes: int64(len(newer)), Body: bytes.NewReader(newer)}); err != nil {
		t.Fatal(err)
	}
	repository := &Repository{objects: store}
	observed, err := repository.readKubernetesFileOriginal(t.Context(), kubernetesFileOrigin{object: receipt})
	defer clear(observed)
	if err != nil || !bytes.Equal(observed, first) {
		t.Fatal("owner byte reader substituted current head", err)
	}
	if err := store.Delete(t.Context(), receipt.Key, receipt.VersionID); err != nil {
		t.Fatal(err)
	}
	if observed, err := repository.readKubernetesFileOriginal(t.Context(), kubernetesFileOrigin{object: receipt}); err == nil || observed != nil {
		clear(observed)
		t.Fatal("missing old version fell back to head")
	}
}

// TestKubernetesFileOriginalDiscardsPartialResponses closes every provider body and withholds bytes on mismatch or uncertainty.
// TestKubernetesFileOriginalDiscardsPartialResponses закрывает каждый provider body и не выдаёт bytes при mismatch либо uncertainty.
func TestKubernetesFileOriginalDiscardsPartialResponses(t *testing.T) {
	for _, mode := range []string{"provider_error", "receipt", "short", "extra", "digest", "close"} {
		t.Run(mode, func(t *testing.T) {
			payload := []byte("synthetic")
			sum := sha256.Sum256(payload)
			receipt := objectstorage.Receipt{Key: "fixture/file", VersionID: "v1", ETag: "etag", Digest: "sha256:" + hex.EncodeToString(sum[:]), SizeBytes: int64(len(payload))}
			body := &fileOriginalBodyFixture{Reader: bytes.NewReader(payload)}
			store := &fileOriginalStoreFixture{object: objectstorage.Object{Receipt: receipt, Body: body}}
			switch mode {
			case "provider_error":
				store.err = objectstorage.ErrUnavailable
			case "receipt":
				store.object.Receipt.VersionID = "v2"
			case "short":
				body.Reader = bytes.NewReader(payload[:2])
			case "extra":
				body.Reader = bytes.NewReader(append(bytes.Clone(payload), 1))
			case "digest":
				body.Reader = bytes.NewReader(bytes.Repeat([]byte{1}, len(payload)))
			case "close":
				body.closeErr = errors.New("synthetic close failure")
			}
			observed, err := (&Repository{objects: store}).readKubernetesFileOriginal(t.Context(), kubernetesFileOrigin{object: receipt})
			if err == nil || observed != nil || !body.closed || store.calls != 1 {
				clear(observed)
				t.Fatal("partial FILE response escaped or replayed", err)
			}
		})
	}
}
