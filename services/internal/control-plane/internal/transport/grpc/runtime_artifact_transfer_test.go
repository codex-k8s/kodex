package grpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type generatedRuntimeArtifact struct {
	size, offset int64
	closes       int
	closeErr     error
}

func (reader *generatedRuntimeArtifact) Read(buffer []byte) (int, error) {
	if reader.offset >= reader.size {
		return 0, io.EOF
	}
	n := int(min(int64(len(buffer)), reader.size-reader.offset))
	for index := range buffer[:n] {
		buffer[index] = byte(((reader.offset + int64(index)) / runtimecontract.MaximumArtifactTransferChunkBytes) % 251)
	}
	reader.offset += int64(n)
	return n, nil
}
func (reader *generatedRuntimeArtifact) Close() error { reader.closes++; return reader.closeErr }

func generatedRuntimeArtifactDigest(t *testing.T, size int64) string {
	t.Helper()
	hash := sha256.New()
	if _, err := io.Copy(hash, &generatedRuntimeArtifact{size: size}); err != nil {
		t.Fatal(err)
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func TestRuntimeArtifactTransferPreservesLargeFileWithoutUnaryBuffer(t *testing.T) {
	const size = int64(33<<20) + 7
	reader := &generatedRuntimeArtifact{size: size}
	digest := generatedRuntimeArtifactDigest(t, size)
	source := platformrepo.ArtifactDownload{Artifact: entity.Artifact{Ref: "art_transferfixture", ProjectRef: "prj_transferfixture",
		FileName: "generated.bin", MediaType: "application/octet-stream", SizeBytes: size, Digest: digest, Revision: 1, Version: 1}, Reader: reader}
	var received int64
	metadata, complete, confirmed := 0, 0, 0
	var first []byte
	hash := sha256.New()
	err := sendExecutionArtifact(t.Context(), source, func(frame *cp.StreamExecutionArtifactResponse) error {
		switch part := frame.GetPart().(type) {
		case *cp.StreamExecutionArtifactResponse_Metadata:
			metadata++
			if received != 0 || complete != 0 || part.Metadata.GetDigest() != digest || part.Metadata.GetSizeBytes() != size {
				t.Fatal("transfer header is not exact")
			}
		case *cp.StreamExecutionArtifactResponse_Chunk:
			if metadata != 1 || complete != 0 || len(part.Chunk) < 1 || len(part.Chunk) > runtimecontract.MaximumArtifactTransferChunkBytes {
				t.Fatal("transfer chunk exceeded bounds or order")
			}
			if first == nil {
				first = part.Chunk
			}
			received += int64(len(part.Chunk))
			_, _ = hash.Write(part.Chunk)
		case *cp.StreamExecutionArtifactResponse_Complete:
			complete++
			if confirmed != 1 || reader.closes != 1 || part.Complete.GetSizeBytes() != size || part.Complete.GetDigest() != digest {
				t.Fatal("transfer completed before owner readback or source close")
			}
		default:
			t.Fatal("unknown transfer frame")
		}
		return nil
	}, func() error { confirmed++; return nil })
	if err != nil || metadata != 1 || complete != 1 || received != size || reader.closes != 1 || "sha256:"+hex.EncodeToString(hash.Sum(nil)) != digest {
		t.Fatalf("large bounded transfer failed: bytes=%d metadata=%d complete=%d closed=%d err=%v", received, metadata, complete, reader.closes, err)
	}
	if len(first) == 0 || first[0] != 0 || first[len(first)-1] != 0 {
		t.Fatal("later reads mutated an already sent chunk")
	}
}

func TestRuntimeArtifactTransferNeverCompletesPartialOrRevokedSource(t *testing.T) {
	const size = int64(runtimecontract.MaximumArtifactTransferChunkBytes) + 17
	for _, scenario := range []string{"short", "extra", "digest", "revoked", "cancel", "send failure", "close failure", "oversize"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			reader := &generatedRuntimeArtifact{size: size}
			source := platformrepo.ArtifactDownload{Artifact: entity.Artifact{Ref: "art_transferfixture", SizeBytes: size, Digest: generatedRuntimeArtifactDigest(t, size)}, Reader: reader}
			switch scenario {
			case "short":
				reader.size--
			case "extra":
				reader.size++
			case "digest":
				source.Artifact.Digest = "sha256:" + strings.Repeat("f", 64)
			case "close failure":
				reader.closeErr = errors.New("synthetic source close failure")
			case "oversize":
				source.Artifact.SizeBytes = runtimecontract.MaximumArtifactTransferBytes + 1
			}
			complete, chunks := 0, 0
			err := sendExecutionArtifact(ctx, source, func(frame *cp.StreamExecutionArtifactResponse) error {
				if frame.GetComplete() != nil {
					complete++
				}
				if len(frame.GetChunk()) != 0 {
					chunks++
					if scenario == "cancel" {
						cancel()
					}
					if scenario == "send failure" {
						return status.Error(codes.Unavailable, "synthetic receiver unavailable")
					}
				}
				return nil
			}, func() error {
				if scenario == "revoked" {
					return errs.ErrForbidden
				}
				return nil
			})
			if err == nil || complete != 0 || reader.closes != 1 {
				t.Fatalf("incomplete transfer accepted: complete=%d closed=%d error=%v", complete, reader.closes, err)
			}
			if scenario == "cancel" && (status.Code(err) != codes.Canceled || chunks != 1) {
				t.Fatal("canceled transfer kept reading")
			}
			if scenario == "oversize" && reader.offset != 0 {
				t.Fatal("oversize transfer read source bytes")
			}
		})
	}
}
