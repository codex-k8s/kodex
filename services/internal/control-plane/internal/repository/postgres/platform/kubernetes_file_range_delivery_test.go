package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// fileRangeDeliveryStore records storage operations independently of metadata-stage creation.
// It proves the byte reader's Range-only behavior, not the PostgreSQL durable Claim ordering.
// fileRangeDeliveryStore фиксирует storage operations независимо от создания metadata stage.
// Подтверждает Range-only byte reader, не PostgreSQL durable Claim ordering.
type fileRangeDeliveryStore struct {
	objectstorage.Store
	reader             objectstorage.RangeReader
	ranges, gets, puts int
	mode               string
}

// Get records a forbidden full source read so a regression cannot silently substitute it for Range.
// Get фиксирует запрещённое полное чтение source, не позволяя незаметно подменить Range.
func (s *fileRangeDeliveryStore) Get(context.Context, string, string) (objectstorage.Object, error) {
	s.gets++
	return objectstorage.Object{}, objectstorage.ErrUnavailable
}

// Put records a forbidden per-chunk object write without performing it.
// Put фиксирует запрещённый per-chunk object write без его выполнения.
func (s *fileRangeDeliveryStore) Put(context.Context, objectstorage.PutInput) (objectstorage.Receipt, error) {
	s.puts++
	return objectstorage.Receipt{}, objectstorage.ErrUnavailable
}

// ReadRange transfers a bounded fixture slice and can inject malformed provider results.
// ReadRange передаёт bounded fixture slice и позволяет подставить malformed provider results.
func (s *fileRangeDeliveryStore) ReadRange(ctx context.Context, r objectstorage.RangeRead) ([]byte, error) {
	s.ranges++
	body, err := s.reader.ReadRange(ctx, r)
	if err != nil {
		return body, err
	}
	switch s.mode {
	case "short":
		return body[:len(body)-1], nil
	case "corrupt":
		body[0] ^= 1
	case "extra":
		body = append(body, 0)
	case "partial":
		return body, objectstorage.ErrUnavailable
	}
	return body, nil
}

// TestKubernetesFileRangeDeliveryUsesOneImmutableObject checks old-version bytes, exact slice hashes and no per-chunk PUT/full Get.
// Database fencing, source ACL and durable attempt ordering remain separate mandatory component tests.
// TestKubernetesFileRangeDeliveryUsesOneImmutableObject проверяет old-version bytes, exact slice hashes и отсутствие per-chunk PUT/full Get.
// Database fencing, source ACL и durable attempt ordering проверяются отдельными обязательными component tests.
func TestKubernetesFileRangeDeliveryUsesOneImmutableObject(t *testing.T) {
	memory := objectstoragetest.NewVersioned()
	body := []byte("original normalized source")
	sum := sha256.Sum256(body)
	receipt, err := memory.Put(t.Context(), objectstorage.PutInput{Key: "fixture/file-source", MediaType: "application/x-tar", Digest: "sha256:" + hex.EncodeToString(sum[:]), SizeBytes: int64(len(body)), Body: bytes.NewReader(body)})
	if err != nil {
		t.Fatal(err)
	}
	newer := []byte("later head")
	newSum := sha256.Sum256(newer)
	if _, err := memory.Put(t.Context(), objectstorage.PutInput{Key: receipt.Key, MediaType: "application/x-tar", Digest: "sha256:" + hex.EncodeToString(newSum[:]), SizeBytes: int64(len(newer)), Body: bytes.NewReader(newer)}); err != nil {
		t.Fatal(err)
	}
	chunk := body[2:8]
	chunkSum := sha256.Sum256(chunk)
	digest := hex.EncodeToString(chunkSum[:])
	stage := kubernetesInputStage{metadata: entity.KubernetesStreamInputSubmission{Channel: "FILE_CONTENT", ByteCount: uint32(len(chunk)), PayloadDigest: digest}}
	read := objectstorage.RangeRead{Object: receipt, Offset: 2, Length: int64(len(chunk)), Digest: "sha256:" + digest}
	for _, mode := range []string{"valid", "short", "corrupt", "extra", "partial"} {
		t.Run(mode, func(t *testing.T) {
			store := &fileRangeDeliveryStore{Store: memory, reader: memory, mode: mode}
			repository := &Repository{objects: store}
			observed, err := repository.readKubernetesInputPayload(t.Context(), stage, read)
			defer clear(observed)
			if mode == "valid" {
				if err != nil || !bytes.Equal(observed, chunk) {
					t.Fatal("exact immutable range was replaced", err)
				}
			} else if err == nil || observed != nil {
				t.Fatal("malformed range leaked bytes", err)
			}
			if store.ranges != 1 || store.gets != 0 || store.puts != 0 {
				t.Fatal("FILE byte reader created or read another object")
			}
			if err := repository.putKubernetesInputReservation(t.Context(), scope{}, kubernetesInputReservation{key: "forbidden"}, command.KubernetesStreamInputSubmit{Channel: "FILE_CONTENT"}); err == nil || store.puts != 0 {
				t.Fatal("obsolete per-chunk PUT path reopened")
			}
		})
	}
}
