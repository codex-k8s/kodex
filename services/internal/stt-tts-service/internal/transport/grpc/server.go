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
	"time"

	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/sttapi"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/libs/go/sttapi/modelprofile"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/authorization"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/errs"
	transcriptionservice "github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/service/transcription"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/transport/grpc/casters"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Service interface {
	Transcribe(context.Context, transcriptionservice.Input) (value.TranscriptionResult, error)
	CheckLocal(context.Context) error
	CheckProtectedPath(context.Context) error
	CheckAvailability(context.Context, value.Principal, string) (transcriptionservice.Availability, error)
	Catalog() modelprofile.Catalog
	GetModelCatalog(context.Context, value.Principal) (modelprofile.Catalog, error)
}

type Readiness interface {
	Ready() (bool, string)
}

type principalResolver func(context.Context, string) (value.Principal, error)

type Server struct {
	sttv1.UnimplementedSpeechToTextServiceServer
	service        Service
	spoolDirectory string
	readiness      Readiness
	requestTimeout time.Duration
	principal      principalResolver
	admission      *byteAdmission
}

func New(service Service, spoolDirectory string, readiness Readiness, requestTimeout time.Duration) (*Server, error) {
	if service == nil || readiness == nil || !filepath.IsAbs(spoolDirectory) || filepath.Clean(spoolDirectory) != spoolDirectory ||
		requestTimeout < time.Second || requestTimeout > 20*time.Second {
		return nil, errors.New("transcription transport configuration is invalid")
	}
	return &Server{
		service: service, spoolDirectory: spoolDirectory, readiness: readiness,
		requestTimeout: requestTimeout, principal: authorization.Principal,
		admission: &byteAdmission{},
	}, nil
}

// StreamServerInterceptor проверяет server-owned authority, резервирует общий
// stream slot до первого Recv и задаёт deadline на полный handler.
func (server *Server) StreamServerInterceptor() googlegrpc.StreamServerInterceptor {
	return func(service any, stream googlegrpc.ServerStream, info *googlegrpc.StreamServerInfo, handler googlegrpc.StreamHandler) error {
		principal, err := server.principal(stream.Context(), info.FullMethod)
		if err != nil {
			return statusError(codes.Unauthenticated, "verified STT authorization context is required", "UNAUTHENTICATED")
		}
		now := time.Now()
		deadline := now.Add(server.requestTimeout)
		if principal.ExpiresAt.Before(deadline) {
			deadline = principal.ExpiresAt
		}
		if !now.Before(deadline) {
			return transportError(context.DeadlineExceeded)
		}
		reservation, ok := server.admission.acquireStream()
		if !ok {
			return statusError(codes.ResourceExhausted, "transcription capacity is exhausted", "RATE_LIMITED")
		}
		defer reservation.release()
		ctx, cancel := context.WithDeadline(stream.Context(), deadline)
		defer cancel()
		ctx = context.WithValue(ctx, streamReservationContextKey{}, reservation)
		return handler(service, &deadlineServerStream{ServerStream: stream, ctx: ctx})
	}
}

func (server *Server) Transcribe(stream sttv1.SpeechToTextService_TranscribeServer) error {
	ctx := stream.Context()
	principal, err := authorization.Principal(ctx, sttv1.SpeechToTextService_Transcribe_FullMethodName)
	if err != nil {
		return statusError(codes.Unauthenticated, "verified STT authorization context is required", "UNAUTHENTICATED")
	}
	reservation, ok := ctx.Value(streamReservationContextKey{}).(*streamReservation)
	if !ok || reservation == nil {
		return statusError(codes.Internal, "transcription admission is missing", "INTERNAL")
	}
	metadataMessage, err := stream.Recv()
	if err != nil {
		if ctx.Err() != nil {
			return transportError(ctx.Err())
		}
		return transportError(errs.ErrInvalidRequest)
	}
	if metadataMessage.GetAvailabilityCheck() != nil {
		if trailing, err := stream.Recv(); err != io.EOF || trailing != nil {
			return transportError(errs.ErrInvalidRequest)
		}
		if ready, _ := server.readiness.Ready(); !ready {
			return transportError(errs.ErrProviderUnavailable)
		}
		availability, err := server.service.CheckAvailability(ctx, principal, sharedobservability.CorrelationID(ctx))
		response := &sttv1.CheckProtectedPathResponse{Stage: protectedStage(availability.Stage), Catalog: sttapi.ModelCatalog(server.service.Catalog())}
		if err == nil {
			response.Ready = true
			response.ValidUntil = timestamppb.New(availability.ValidUntil)
		}
		if ctx.Err() != nil {
			return transportError(ctx.Err())
		}
		return stream.SendAndClose(&sttv1.TranscribeResponse{Availability: response})
	}
	if metadataMessage.GetMetadata() == nil || metadataMessage.GetMetadata().GetSizeBytes() == 0 ||
		metadataMessage.GetMetadata().GetSizeBytes() > uint64(value.MaximumAbsoluteBytes) {
		return transportError(errs.ErrInvalidRequest)
	}
	sizeBytes := int64(metadataMessage.GetMetadata().GetSizeBytes())
	if !reservation.reserveBytes(sizeBytes) {
		return statusError(codes.ResourceExhausted, "transcription capacity is exhausted", "RATE_LIMITED")
	}
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
		principal, sharedobservability.CorrelationID(ctx), spool, sizeBytes, metadataMessage.GetMetadata().GetMediaType(),
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
			if stream.Context().Err() != nil {
				return 0, "", stream.Context().Err()
			}
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
	ready, _ := server.readiness.Ready()
	if !ready {
		return &sttv1.CheckReadinessResponse{Ready: false}, statusError(codes.Unavailable, "STT local runtime is unavailable", "UNAVAILABLE")
	}
	return &sttv1.CheckReadinessResponse{Ready: true}, nil
}

func (server *Server) GetModelCatalog(ctx context.Context, request *sttv1.GetModelCatalogRequest) (*sttv1.GetModelCatalogResponse, error) {
	principal, err := authorization.Principal(ctx, sttv1.SpeechToTextService_GetModelCatalog_FullMethodName)
	if err != nil {
		return nil, statusError(codes.Unauthenticated, "verified STT authorization context is required", "UNAUTHENTICATED")
	}
	if request == nil || len(request.ProtoReflect().GetUnknown()) != 0 {
		return nil, transportError(errs.ErrInvalidRequest)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ctx, cancelAuthority := context.WithDeadline(ctx, principal.ExpiresAt)
	defer cancelAuthority()
	catalog, err := server.service.GetModelCatalog(ctx, principal)
	if err != nil {
		return nil, transportError(err)
	}
	if err := ctx.Err(); err != nil {
		return nil, transportError(err)
	}
	return &sttv1.GetModelCatalogResponse{Catalog: sttapi.ModelCatalog(catalog)}, nil
}

func (server *Server) CheckProtectedPath(ctx context.Context, _ *sttv1.CheckProtectedPathRequest) (*sttv1.CheckProtectedPathResponse, error) {
	return &sttv1.CheckProtectedPathResponse{Ready: false, Stage: sttv1.ProtectedPathStage_PROTECTED_PATH_STAGE_DELEGATED_AUTHORITY}, nil
}

func protectedStage(stage value.Stage) sttv1.ProtectedPathStage {
	switch stage {
	case value.StagePolicy:
		return sttv1.ProtectedPathStage_PROTECTED_PATH_STAGE_POLICY
	case value.StageCredential:
		return sttv1.ProtectedPathStage_PROTECTED_PATH_STAGE_CREDENTIAL
	case value.StageEgress:
		return sttv1.ProtectedPathStage_PROTECTED_PATH_STAGE_EGRESS
	case value.StageProvider:
		return sttv1.ProtectedPathStage_PROTECTED_PATH_STAGE_PROVIDER
	case value.StageSuccess:
		return sttv1.ProtectedPathStage_PROTECTED_PATH_STAGE_READY
	default:
		return sttv1.ProtectedPathStage_PROTECTED_PATH_STAGE_DELEGATED_AUTHORITY
	}
}

type byteAdmission struct {
	mu      sync.Mutex
	streams int
	bytes   int64
}

type streamReservationContextKey struct{}

type streamReservation struct {
	admission *byteAdmission
	once      sync.Once
	bytes     int64
}

func (admission *byteAdmission) acquireStream() (*streamReservation, bool) {
	admission.mu.Lock()
	defer admission.mu.Unlock()
	if admission.streams >= value.MaximumConcurrentStreams {
		return nil, false
	}
	admission.streams++
	return &streamReservation{admission: admission}, true
}

func (admission *byteAdmission) snapshot() (int, int64) {
	admission.mu.Lock()
	defer admission.mu.Unlock()
	return admission.streams, admission.bytes
}

func (reservation *streamReservation) reserveBytes(size int64) bool {
	if reservation == nil || reservation.admission == nil || reservation.bytes != 0 {
		return false
	}
	admission := reservation.admission
	admission.mu.Lock()
	defer admission.mu.Unlock()
	if size <= 0 || size > value.MaximumAbsoluteBytes || admission.bytes > value.MaximumInflightBytes-size {
		return false
	}
	admission.bytes += size
	reservation.bytes = size
	return true
}

func (reservation *streamReservation) release() {
	if reservation == nil || reservation.admission == nil {
		return
	}
	reservation.once.Do(func() {
		admission := reservation.admission
		admission.mu.Lock()
		defer admission.mu.Unlock()
		admission.streams--
		admission.bytes -= reservation.bytes
	})
}

type deadlineServerStream struct {
	googlegrpc.ServerStream
	ctx context.Context
}

func (stream *deadlineServerStream) Context() context.Context { return stream.ctx }

func (stream *deadlineServerStream) RecvMsg(message any) error {
	return awaitStreamOperation(stream.ctx, func() error { return stream.ServerStream.RecvMsg(message) })
}

func (stream *deadlineServerStream) SendMsg(message any) error {
	return awaitStreamOperation(stream.ctx, func() error { return stream.ServerStream.SendMsg(message) })
}

func awaitStreamOperation(ctx context.Context, operation func() error) error {
	done := make(chan error, 1)
	go func() { done <- operation() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return status.FromContextError(ctx.Err()).Err()
	}
}
