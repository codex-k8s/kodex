// Package grpc реализует тонкий transport STT.
package grpc

import (
	"context"
	"errors"

	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/authorization"
	transcriptionservice "github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/service/transcription"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Service interface {
	Transcribe(context.Context, transcriptionservice.Input) (string, error)
	Check(context.Context) error
}

type Server struct {
	sttv1.UnimplementedSpeechToTextServiceServer
	service Service
	slots   chan struct{}
}

const maximumConcurrentTranscriptions = 2

func New(service Service) (*Server, error) {
	if service == nil {
		return nil, errors.New("transcription domain service is required")
	}
	return &Server{service: service, slots: make(chan struct{}, maximumConcurrentTranscriptions)}, nil
}

func (server *Server) Transcribe(ctx context.Context, request *sttv1.TranscribeRequest) (*sttv1.TranscribeResponse, error) {
	principal, err := authorization.Principal(ctx, sttv1.SpeechToTextService_Transcribe_FullMethodName)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "verified STT authorization context is required")
	}
	if !server.acquire() {
		return nil, statusError(codes.ResourceExhausted, "transcription capacity is exhausted", "RATE_LIMITED")
	}
	defer server.release()
	text, err := server.service.Transcribe(ctx, transcriptionservice.Input{
		Principal: principal, RequestID: principal.RequestID,
		Audio: request.GetAudio(), MediaType: request.GetMediaType(),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &sttv1.TranscribeResponse{Text: text}, nil
}

func (server *Server) acquire() bool {
	select {
	case server.slots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (server *Server) release() {
	<-server.slots
}

func (server *Server) CheckReadiness(ctx context.Context, _ *sttv1.CheckReadinessRequest) (*sttv1.CheckReadinessResponse, error) {
	if err := server.service.Check(ctx); err != nil {
		return &sttv1.CheckReadinessResponse{Ready: false}, statusError(codes.Unavailable, "STT dependencies are unavailable", "UNAVAILABLE")
	}
	return &sttv1.CheckReadinessResponse{Ready: true}, nil
}
