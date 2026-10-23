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

// leaseSpeechClient implements only a synthetic probe stream for availability projection; embedded methods are not a configured STT provider.
// leaseSpeechClient реализует только тестовый probe stream для проекции доступности; встроенные методы не являются настроенным STT-провайдером.
type leaseSpeechClient struct {
	sttv1.SpeechToTextServiceClient
}

// Transcribe creates a synthetic stream retaining the supplied probe context, without opening a provider connection or processing audio.
// Transcribe создаёт тестовый stream с переданным probe-контекстом без соединения с провайдером и обработки аудио.
func (*leaseSpeechClient) Transcribe(ctx context.Context, _ ...grpc.CallOption) (sttv1.SpeechToTextService_TranscribeClient, error) {
	return &leaseSpeechStream{ctx: ctx}, nil
}

// leaseSpeechStream retains the probe context and embeds unused stream methods; it returns only fixture availability metadata, not a transcription.
// leaseSpeechStream хранит probe-контекст и встраивает неиспользуемые методы потока; возвращается только тестовая metadata доступности, а не транскрипция.
type leaseSpeechStream struct {
	grpc.ClientStream
	ctx context.Context
}

// Send accepts any synthetic request as a no-op, without validating, persisting or transmitting its content.
// Send принимает любой тестовый запрос без действий, проверки, сохранения и передачи содержимого.
func (*leaseSpeechStream) Send(*sttv1.TranscribeRequest) error { return nil }

// CloseAndRecv reports synthetic READY with ValidUntil equal to the probe deadline, or an empty response when unbounded; it does not verify a protected backend.
// CloseAndRecv возвращает тестовый READY с ValidUntil по сроку probe либо пустой ответ без срока; защищённый backend не проверяется.
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

// TestRealtimeSpeechAvailabilityOutlivesHeartbeat checks the projected synthetic STT availability is usable and its parsed validity outlives one heartbeat interval; no real microphone, audio or model is used.
// TestRealtimeSpeechAvailabilityOutlivesHeartbeat проверяет доступность тестовой STT-проекции и её разобранный срок больше одного heartbeat; настоящие микрофон, аудио и модель не используются.
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
