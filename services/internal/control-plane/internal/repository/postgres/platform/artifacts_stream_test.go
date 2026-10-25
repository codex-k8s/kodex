package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
)

// seekRequiredObjectStore wraps the in-memory object store and records exact incoming reader plus
// whether it remains seekable. It is a streaming-adapter fixture, not a remote storage or
// durability implementation.
// seekRequiredObjectStore оборачивает in-memory object store и записывает exact incoming reader с
// признаком seekable. Это streaming-adapter fixture, а не remote storage либо durability
// implementation.
type seekRequiredObjectStore struct {
	*objectstoragetest.Store
	sawSeekable  bool
	receivedBody io.Reader
}

// Put captures the input body and rejects it unless it implements io.Seeker, otherwise delegates
// unchanged input/context to the in-memory store. It adds no real object authorization or network
// behavior.
// Put сохраняет input body и отклоняет его без io.Seeker, иначе передаёт неизменные input/context
// in-memory store. Не добавляет real object authorization либо network behavior.
func (store *seekRequiredObjectStore) Put(ctx context.Context, input objectstorage.PutInput) (objectstorage.Receipt, error) {
	store.receivedBody = input.Body
	_, store.sawSeekable = input.Body.(io.Seeker)
	if !store.sawSeekable {
		return objectstorage.Receipt{}, objectstorage.ErrInvalid
	}
	return store.Store.Put(ctx, input)
}

// TestPutArtifactObjectKeepsStreamingBodySeekable requires the original seekable reader to reach
// storage unchanged with exact digest/size and remain rewound after verification. The in-memory
// regression does not prove remote upload/readback behavior or benchmark memory use.
// TestPutArtifactObjectKeepsStreamingBodySeekable требует передачи original seekable reader в
// storage без замены с exact digest/size и сохранения rewind после verification. In-memory
// regression не доказывает remote upload/readback behavior и не измеряет memory use.
func TestPutArtifactObjectKeepsStreamingBodySeekable(t *testing.T) {
	t.Parallel()

	const body = "streamed artifact"
	sum := sha256.Sum256([]byte(body))
	digest := "sha256:" + hex.EncodeToString(sum[:])
	reader := strings.NewReader(body)
	store := &seekRequiredObjectStore{Store: objectstoragetest.New()}
	repository := &Repository{objects: store}

	receipt, err := repository.putArtifactObject(t.Context(), "organizations/org/artifacts/art/1", platformrepo.ArtifactUpload{
		MediaType: "text/plain", Digest: digest, SizeBytes: int64(len(body)), Reader: reader,
	})
	if err != nil {
		t.Fatalf("put seekable artifact object: %v", err)
	}
	if !store.sawSeekable || store.receivedBody != reader || receipt.Digest != digest || receipt.SizeBytes != int64(len(body)) {
		t.Fatalf("unexpected storage input or receipt: seekable=%t same_reader=%t receipt=%#v", store.sawSeekable, store.receivedBody == reader, receipt)
	}
	restored, err := io.ReadAll(reader)
	if err != nil || string(restored) != body {
		t.Fatalf("reader was not restored after verification: body=%q err=%v", restored, err)
	}
}

// TestPutArtifactObjectDeletesFailedReadback uploads synthetic bytes with a deliberately wrong
// digest and requires Conflict plus no remaining object in the in-memory store. It guards
// failed-readback compensation locally without proving real versioned object deletion.
// TestPutArtifactObjectDeletesFailedReadback загружает synthetic bytes с намеренно неверным digest
// и требует Conflict с отсутствием object в in-memory store. Локально проверяет failed-readback
// compensation без доказательства real versioned object deletion.
func TestPutArtifactObjectDeletesFailedReadback(t *testing.T) {
	t.Parallel()

	const body = "streamed artifact"
	store := &seekRequiredObjectStore{Store: objectstoragetest.New()}
	repository := &Repository{objects: store}
	key := "organizations/org/artifacts/art/2"

	_, err := repository.putArtifactObject(t.Context(), key, platformrepo.ArtifactUpload{
		MediaType: "text/plain", Digest: "sha256:" + strings.Repeat("a", 64),
		SizeBytes: int64(len(body)), Reader: strings.NewReader(body),
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("mismatched artifact digest error = %v, want conflict", err)
	}
	if _, headErr := store.Head(t.Context(), key, ""); headErr != objectstorage.ErrNotFound {
		t.Fatalf("failed artifact object was retained: %v", headErr)
	}
}
