package grpc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	repository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestCreateAttachmentSetDraftRejectsMissingProject checks missing project input is rejected
// before a domain call, using an otherwise unconfigured transport server.
// TestCreateAttachmentSetDraftRejectsMissingProject проверяет отклонение отсутствующего проекта до
// доменного вызова на transport server без настроенных зависимостей.
func TestCreateAttachmentSetDraftRejectsMissingProject(t *testing.T) {
	t.Parallel()
	server := &Server{}
	_, err := server.CreateAttachmentSetDraft(context.Background(), &controlplanev1.CreateAttachmentSetDraftRequest{})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("status = %v, err=%v", status.Code(err), err)
	}
}

// TestReceiveArtifactUploadSpoolsBeyondLegacyLimit checks a seventeen-MiB synthetic upload is
// spooled with exact size, digest and leading bytes and is closed by test cleanup.
// TestReceiveArtifactUploadSpoolsBeyondLegacyLimit проверяет spool синтетического upload размером
// семнадцать MiB с точными размером, digest и начальными байтами и закрытием при очистке теста.
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

// TestReceiveArtifactUploadRejectsDigestMismatchAndOversize checks digest mismatch is
// InvalidArgument and a declared artifact above the byte limit is ResourceExhausted.
// TestReceiveArtifactUploadRejectsDigestMismatchAndOversize проверяет InvalidArgument при
// несовпадении digest и ResourceExhausted при заявленном размере выше лимита артефакта.
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

// TestValidateArtifactUploadScope checks permitted project and organization uploads and rejects
// missing project or project/run locators in organization scope.
// TestValidateArtifactUploadScope проверяет допустимые project и organization uploads и отказ при
// отсутствии проекта либо project/run ссылках в области организации.
func TestValidateArtifactUploadScope(t *testing.T) {
	t.Parallel()
	project := &controlplanev1.UploadArtifactMetadata{ProjectRef: "prj_test", RunRef: "run_test"}
	organization := &controlplanev1.UploadArtifactMetadata{}
	if err := validateArtifactUploadScope(project, true); err != nil {
		t.Fatalf("project upload scope rejected: %v", err)
	}
	if err := validateArtifactUploadScope(organization, false); err != nil {
		t.Fatalf("organization upload scope rejected: %v", err)
	}
	for name, test := range map[string]struct {
		metadata        *controlplanev1.UploadArtifactMetadata
		projectRequired bool
	}{
		"project without project":   {metadata: organization, projectRequired: true},
		"organization with project": {metadata: project, projectRequired: false},
		"organization with run":     {metadata: &controlplanev1.UploadArtifactMetadata{RunRef: "run_test"}, projectRequired: false},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateArtifactUploadScope(test.metadata, test.projectRequired); status.Code(err) != codes.InvalidArgument {
				t.Fatalf("status = %v, err=%v", status.Code(err), err)
			}
		})
	}
}

// generatedArtifactUploadStream is a handwritten bounded-memory fixture tracking metadata,
// repeated-byte chunks, commit and EOF; its name does not designate generated source.
// generatedArtifactUploadStream является рукописной оснасткой с ограниченной памятью для metadata,
// chunks повторяющегося байта, commit и EOF; имя не обозначает generated source.
type generatedArtifactUploadStream struct {
	size, sent int64
	value      byte
	digest     string
	state      int
}

// newGeneratedArtifactUploadStream computes the repeated-byte payload digest in bounded chunks and
// initializes a synthetic upload without allocating the complete file.
// newGeneratedArtifactUploadStream вычисляет digest повторяющихся байтов ограниченными chunks и
// создаёт синтетический upload без выделения полного файла.
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

// Recv emits one metadata frame, bounded repeated-byte chunks, one exact-size/digest commit and
// then EOF while advancing fixture counters.
// Recv выдаёт один metadata frame, ограниченные chunks повторяющегося байта, один commit с
// размером и digest, затем EOF, обновляя счётчики оснастки.
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
