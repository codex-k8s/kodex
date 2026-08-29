package grpc

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	repository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestReceiveArtifactUploadSpoolsBeyondLegacyLimit(t *testing.T) {
	t.Parallel()
	const size = int64(17 << 20)
	stream := newGeneratedArtifactUploadStream(size, 'a')
	upload, err := receiveArtifactUpload(stream)
	if err != nil {
		t.Fatal(err)
	}
	defer upload.close()
	info, err := upload.file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != size || upload.sha256 != stream.digest {
		t.Fatalf("unexpected spool: size=%d digest=%q", info.Size(), upload.sha256)
	}
	probe := make([]byte, 32)
	if _, err := io.ReadFull(upload.file, probe); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(probe, bytes.Repeat([]byte{'a'}, len(probe))) {
		t.Fatalf("unexpected spooled bytes: %q", probe)
	}
}

func TestReceiveArtifactUploadRejectsDigestMismatchAndOversize(t *testing.T) {
	t.Parallel()
	mismatch := newGeneratedArtifactUploadStream(64, 'a')
	mismatch.digest = hex.EncodeToString(bytes.Repeat([]byte{0xff}, sha256.Size))
	if _, err := receiveArtifactUpload(mismatch); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("digest mismatch code = %v, err=%v", status.Code(err), err)
	}
	oversize := &generatedArtifactUploadStream{size: repository.MaximumArtifactBytes + 1, value: 'a'}
	if _, err := receiveArtifactUpload(oversize); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("oversize code = %v, err=%v", status.Code(err), err)
	}
}

type generatedArtifactUploadStream struct {
	size, sent int64
	value      byte
	digest     string
	state      int
}

func newGeneratedArtifactUploadStream(size int64, value byte) *generatedArtifactUploadStream {
	hash := sha256.New()
	chunk := bytes.Repeat([]byte{value}, 64<<10)
	remaining := size
	for remaining > 0 {
		count := int64(len(chunk))
		if count > remaining {
			count = remaining
		}
		_, _ = hash.Write(chunk[:count])
		remaining -= count
	}
	return &generatedArtifactUploadStream{size: size, value: value, digest: hex.EncodeToString(hash.Sum(nil))}
}

func (stream *generatedArtifactUploadStream) Recv() (*controlplanev1.UploadArtifactRequest, error) {
	if stream.state == 0 {
		stream.state = 1
		return &controlplanev1.UploadArtifactRequest{Part: &controlplanev1.UploadArtifactRequest_Metadata{Metadata: &controlplanev1.UploadArtifactMetadata{
			ProjectRef: "prj_test", FileName: "large.txt", MediaType: "text/plain", SizeBytes: stream.size,
		}}}, nil
	}
	if stream.sent < stream.size {
		count := int64(64 << 10)
		if remaining := stream.size - stream.sent; count > remaining {
			count = remaining
		}
		stream.sent += count
		return &controlplanev1.UploadArtifactRequest{Part: &controlplanev1.UploadArtifactRequest_Chunk{Chunk: bytes.Repeat([]byte{stream.value}, int(count))}}, nil
	}
	if stream.state == 1 {
		stream.state = 2
		return &controlplanev1.UploadArtifactRequest{Part: &controlplanev1.UploadArtifactRequest_Commit{Commit: &controlplanev1.UploadArtifactCommit{
			SizeBytes: stream.size,
			Sha256:    stream.digest,
		}}}, nil
	}
	return nil, io.EOF
}
