// Package grpc реализует тонкий transport STT.
package grpc

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sync"

	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/authorization"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/errs"
	transcriptionservice "github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/service/transcription"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/transport/grpc/casters"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
)

type Service interface {
	Transcribe(context.Context, transcriptionservice.Input) (value.TranscriptionResult, error)
	CheckLocal(context.Context) error
	CheckProtectedPath(context.Context) error
}

type Server struct {
	sttv1.UnimplementedSpeechToTextServiceServer
	service        Service
	spoolDirectory string
	admission      *byteAdmission
}

func New(service Service, spoolDirectory string) (*Server, error) {
	if service == nil || !filepath.IsAbs(spoolDirectory) || filepath.Clean(spoolDirectory) != spoolDirectory {
		return nil, errors.New("transcription transport configuration is invalid")
	}
	return &Server{service: service, spoolDirectory: spoolDirectory, admission: &byteAdmission{}}, nil
}

func (server *Server) Transcribe(stream sttv1.SpeechToTextService_TranscribeServer) error {
	ctx := stream.Context()
	principal, err := authorization.Principal(ctx, sttv1.SpeechToTextService_Transcribe_FullMethodName)
	if err != nil {
		return statusError(codes.Unauthenticated, "verified STT authorization context is required", "UNAUTHENTICATED")
	}
	metadataMessage, err := stream.Recv()
	if err != nil || metadataMessage.GetMetadata() == nil || metadataMessage.GetMetadata().GetSizeBytes() == 0 ||
		metadataMessage.GetMetadata().GetSizeBytes() > uint64(value.MaximumAbsoluteBytes) {
		return transportError(errs.ErrInvalidRequest)
	}
	sizeBytes := int64(metadataMessage.GetMetadata().GetSizeBytes())
	if !server.admission.acquire(sizeBytes) {
		return statusError(codes.ResourceExhausted, "transcription capacity is exhausted", "RATE_LIMITED")
	}
	defer server.admission.release(sizeBytes)
	spool, err := os.CreateTemp(server.spoolDirectory, "request-*.audio")
	if err != nil {
		return statusError(codes.Unavailable, "transcription spool is unavailable", "UNAVAILABLE")
	}
	spoolName := spool.Name()
	defer func() {
		_ = spool.Close()
		_ = os.Remove(spoolName)
	}()
	written, digest, err := receiveAudio(stream, spool, sizeBytes)
	if err != nil {
		return transportError(err)
	}
	if written != sizeBytes || digest == "" {
		return transportError(errs.ErrInvalidRequest)
	}
	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		return statusError(codes.Unavailable, "transcription spool is unavailable", "UNAVAILABLE")
	}
	result, err := server.service.Transcribe(ctx, casters.TranscriptionInput(
		principal, uuid.NewString(), spool, sizeBytes, metadataMessage.GetMetadata().GetMediaType(),
	))
	if err != nil {
		return transportError(err)
	}
	return stream.SendAndClose(casters.TranscriptionResponse(result))
}

func receiveAudio(stream sttv1.SpeechToTextService_TranscribeServer, output io.Writer, expected int64) (int64, string, error) {
	digest := sha256.New()
	destination := io.MultiWriter(output, digest)
	var written int64
	for {
		message, err := stream.Recv()
		if err != nil {
			return 0, "", errs.ErrInvalidRequest
		}
		if chunk := message.GetChunk(); chunk != nil {
			if len(chunk) == 0 || len(chunk) > value.MaximumChunkBytes || int64(len(chunk)) > expected-written {
				return 0, "", errs.ErrInvalidRequest
			}
			count, writeErr := destination.Write(chunk)
			if writeErr != nil || count != len(chunk) {
				return 0, "", errs.ErrProviderUnavailable
			}
			written += int64(count)
			continue
		}
		commit := message.GetCommit()
		if commit == nil || written != expected || commit.GetSizeBytes() != uint64(expected) || !matchesDigest(digest, commit.GetSha256()) {
			return 0, "", errs.ErrInvalidRequest
		}
		if trailing, trailingErr := stream.Recv(); trailingErr != io.EOF || trailing != nil {
			return 0, "", errs.ErrInvalidRequest
		}
		return written, commit.GetSha256(), nil
	}
}

func matchesDigest(digest hash.Hash, declared string) bool {
	if len(declared) != sha256.Size*2 {
		return false
	}
	actual := make([]byte, hex.EncodedLen(digest.Size()))
	hex.Encode(actual, digest.Sum(nil))
	return subtle.ConstantTimeCompare(actual, []byte(declared)) == 1
}

func (server *Server) CheckReadiness(ctx context.Context, _ *sttv1.CheckReadinessRequest) (*sttv1.CheckReadinessResponse, error) {
	if err := server.service.CheckLocal(ctx); err != nil {
		return &sttv1.CheckReadinessResponse{Ready: false}, statusError(codes.Unavailable, "STT local runtime is unavailable", "UNAVAILABLE")
	}
	return &sttv1.CheckReadinessResponse{Ready: true}, nil
}

func (server *Server) CheckProtectedPath(ctx context.Context, _ *sttv1.CheckProtectedPathRequest) (*sttv1.CheckProtectedPathResponse, error) {
	if err := server.service.CheckProtectedPath(ctx); err != nil {
		return &sttv1.CheckProtectedPathResponse{Ready: false, Stage: sttv1.ProtectedPathStage_PROTECTED_PATH_STAGE_DELEGATED_AUTHORITY},
			statusError(codes.FailedPrecondition, "STT protected path is not materialized", "STATE_CONFLICT")
	}
	return &sttv1.CheckProtectedPathResponse{Ready: true, Stage: sttv1.ProtectedPathStage_PROTECTED_PATH_STAGE_READY}, nil
}

type byteAdmission struct {
	mu      sync.Mutex
	streams int
	bytes   int64
}

func (admission *byteAdmission) acquire(size int64) bool {
	admission.mu.Lock()
	defer admission.mu.Unlock()
	if size <= 0 || size > value.MaximumAbsoluteBytes || admission.streams >= value.MaximumConcurrentStreams ||
		admission.bytes > value.MaximumInflightBytes-size {
		return false
	}
	admission.streams++
	admission.bytes += size
	return true
}

func (admission *byteAdmission) release(size int64) {
	admission.mu.Lock()
	defer admission.mu.Unlock()
	admission.streams--
	admission.bytes -= size
}
