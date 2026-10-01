package websockettransport

import (
	"context"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type leaseSpeechClient struct {
	sttv1.SpeechToTextServiceClient
}

func (*leaseSpeechClient) Transcribe(ctx context.Context, _ ...grpc.CallOption) (sttv1.SpeechToTextService_TranscribeClient, error) {
	return &leaseSpeechStream{ctx: ctx}, nil
}

type leaseSpeechStream struct {
	grpc.ClientStream
	ctx context.Context
}

func (*leaseSpeechStream) Send(*sttv1.TranscribeRequest) error { return nil }

func (stream *leaseSpeechStream) CloseAndRecv() (*sttv1.TranscribeResponse, error) {
	deadline, ok := stream.ctx.Deadline()
	if !ok {
		return &sttv1.TranscribeResponse{}, nil
	}
	return &sttv1.TranscribeResponse{Availability: &sttv1.CheckProtectedPathResponse{
		Ready: true, Stage: sttv1.ProtectedPathStage_PROTECTED_PATH_STAGE_READY,
		ValidUntil: timestamppb.New(deadline),
	}}, nil
}

func TestRealtimeSpeechAvailabilityOutlivesHeartbeat(t *testing.T) {
	started := time.Now()
	server := &Server{speech: &leaseSpeechClient{}}
	availability := server.projectSpeechAvailability(t.Context(), &controlplanev1.SpeechTranscriptionAvailability{Eligible: true})
	if !availability.Available || availability.ValidUntil == nil {
		t.Fatalf("realtime STT availability rejected: %+v", availability)
	}
	deadline, err := time.Parse(time.RFC3339Nano, *availability.ValidUntil)
	if err != nil {
		t.Fatalf("parse STT lease deadline: %v", err)
	}
	if remaining := deadline.Sub(started); remaining <= heartbeatInterval {
		t.Fatalf("STT lease %s does not outlive heartbeat %s", remaining, heartbeatInterval)
	}
}
