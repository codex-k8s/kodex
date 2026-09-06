package callback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

type transferPatternReader struct{ remaining, offset int64 }

func (reader *transferPatternReader) Read(buffer []byte) (int, error) {
	if reader.remaining == 0 {
		return 0, io.EOF
	}
	n := int(min(int64(len(buffer)), reader.remaining))
	for index := range buffer[:n] {
		buffer[index] = byte((reader.offset + int64(index)) % 251)
	}
	reader.remaining -= int64(n)
	reader.offset += int64(n)
	return n, nil
}

func TestArtifactTransferSpoolsLargeFileBeforeSuccessfulReturn(t *testing.T) {
	const size = int64(33<<20) + 7
	hash := sha256.New()
	if _, err := io.Copy(hash, &transferPatternReader{remaining: size}); err != nil {
		t.Fatal(err)
	}
	pin := artifactTransferPin{ref: "art_largefixture", project: "prj_fixture", revision: 1, size: size, digest: "sha256:" + hex.EncodeToString(hash.Sum(nil))}
	spool := fixtureArtifactSpool(t)
	file, release, err := spool.acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	reader := &transferPatternReader{remaining: size}
	phase, chunks := 0, 0
	recv := func() (*cp.StreamExecutionArtifactResponse, error) {
		if phase == 0 {
			phase++
			return &cp.StreamExecutionArtifactResponse{Part: &cp.StreamExecutionArtifactResponse_Metadata{Metadata: &cp.Artifact{
				Ref: pin.ref, ProjectRef: pin.project, Revision: 1, SizeBytes: size, Digest: pin.digest}}}, nil
		}
		if reader.remaining > 0 {
			buffer := make([]byte, min(int64(runtimecontract.MaximumArtifactTransferChunkBytes), reader.remaining))
			if _, err := io.ReadFull(reader, buffer); err != nil {
				t.Fatal(err)
			}
			chunks++
			return &cp.StreamExecutionArtifactResponse{Part: &cp.StreamExecutionArtifactResponse_Chunk{Chunk: buffer}}, nil
		}
		if phase == 1 {
			phase++
			return &cp.StreamExecutionArtifactResponse{Part: &cp.StreamExecutionArtifactResponse_Complete{Complete: &cp.RuntimeArtifactTransferComplete{SizeBytes: size, Digest: pin.digest}}}, nil
		}
		phase++
		return nil, io.EOF
	}
	if err := receiveArtifactTransfer(t.Context(), recv, file, pin); err != nil || phase != 3 || chunks < 512 {
		t.Fatalf("large stream rejected: %v", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	hash.Reset()
	if n, err := io.Copy(hash, file); err != nil || n != size || "sha256:"+hex.EncodeToString(hash.Sum(nil)) != pin.digest {
		t.Fatal("spooled file differs from accepted stream")
	}
	release()
	if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("successful spool descriptor remains open")
	}
}

func TestArtifactTransferRejectsMissingTerminalCorruptionAndUnexpectedFrames(t *testing.T) {
	for _, scenario := range []string{"metadata mismatch", "duplicate metadata", "empty chunk", "oversize chunk", "extra bytes", "short bytes", "corrupt bytes", "missing complete", "complete mismatch", "after complete", "unknown frame", "receiver error", "disk failure", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			content := []byte("synthetic private file")
			sum := sha256.Sum256(content)
			pin := artifactTransferPin{ref: "art_transferfixture", project: "prj_fixture", revision: 1, size: int64(len(content)), digest: "sha256:" + hex.EncodeToString(sum[:])}
			header := &cp.StreamExecutionArtifactResponse{Part: &cp.StreamExecutionArtifactResponse_Metadata{Metadata: &cp.Artifact{Ref: pin.ref, ProjectRef: pin.project, Revision: 1, SizeBytes: pin.size, Digest: pin.digest}}}
			chunk := &cp.StreamExecutionArtifactResponse{Part: &cp.StreamExecutionArtifactResponse_Chunk{Chunk: content}}
			complete := &cp.StreamExecutionArtifactResponse{Part: &cp.StreamExecutionArtifactResponse_Complete{Complete: &cp.RuntimeArtifactTransferComplete{SizeBytes: pin.size, Digest: pin.digest}}}
			frames := []*cp.StreamExecutionArtifactResponse{header, chunk, complete}
			switch scenario {
			case "metadata mismatch":
				header.GetMetadata().ProjectRef = "prj_foreign"
			case "duplicate metadata":
				frames[1] = header
			case "empty chunk":
				chunk.Part = &cp.StreamExecutionArtifactResponse_Chunk{}
			case "oversize chunk":
				chunk.Part = &cp.StreamExecutionArtifactResponse_Chunk{Chunk: make([]byte, runtimecontract.MaximumArtifactTransferChunkBytes+1)}
			case "extra bytes":
				chunk.Part = &cp.StreamExecutionArtifactResponse_Chunk{Chunk: append(content, 0)}
			case "short bytes":
				chunk.Part = &cp.StreamExecutionArtifactResponse_Chunk{Chunk: content[:len(content)-1]}
			case "corrupt bytes":
				content[0] ^= 0xff
			case "missing complete":
				frames = frames[:2]
			case "complete mismatch":
				complete.GetComplete().SizeBytes++
			case "after complete":
				frames = append(frames, chunk)
			case "unknown frame":
				frames[1] = &cp.StreamExecutionArtifactResponse{}
			}
			spool := fixtureArtifactSpool(t)
			file, release, err := spool.acquire(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			recv := func() (*cp.StreamExecutionArtifactResponse, error) {
				calls++
				if scenario == "receiver error" && calls == 2 {
					return nil, errors.New("synthetic transport failure")
				}
				if len(frames) == 0 {
					return nil, io.EOF
				}
				frame := frames[0]
				frames = frames[1:]
				if scenario == "canceled" && calls == 1 {
					cancel()
				}
				return frame, nil
			}
			if scenario == "disk failure" {
				_ = file.Close()
			}
			if err := receiveArtifactTransfer(ctx, recv, file, pin); err == nil {
				t.Fatal("invalid stream accepted")
			}
			release()
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) || len(spool.slots) != 0 {
				t.Fatal("failed transfer did not release resources")
			}
		})
	}
}

func TestArtifactTransferMissingEOFCannotOutliveDeadline(t *testing.T) {
	sum := sha256.Sum256(nil)
	pin := artifactTransferPin{ref: "art_emptyfixture", project: "prj_fixture", revision: 1, digest: "sha256:" + hex.EncodeToString(sum[:])}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	calls := 0
	recv := func() (*cp.StreamExecutionArtifactResponse, error) {
		calls++
		if calls == 1 {
			return &cp.StreamExecutionArtifactResponse{Part: &cp.StreamExecutionArtifactResponse_Metadata{Metadata: &cp.Artifact{Ref: pin.ref, ProjectRef: pin.project, Revision: 1, Digest: pin.digest}}}, nil
		}
		if calls == 2 {
			return &cp.StreamExecutionArtifactResponse{Part: &cp.StreamExecutionArtifactResponse_Complete{Complete: &cp.RuntimeArtifactTransferComplete{Digest: pin.digest}}}, nil
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if err := receiveArtifactTransfer(ctx, recv, io.Discard, pin); err == nil || calls != 3 {
		t.Fatal("missing EOF accepted or deadline ignored")
	}
}
