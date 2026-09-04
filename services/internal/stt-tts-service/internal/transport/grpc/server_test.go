package grpc

import (
	"context"
	"testing"

	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	transcriptionservice "github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/service/transcription"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeService struct{}

func (fakeService) Transcribe(context.Context, transcriptionservice.Input) (string, error) {
	return "unexpected", nil
}
func (fakeService) Check(context.Context) error { return nil }

func TestTranscribeRejectsMissingVerifiedContext(t *testing.T) {
	server, err := New(fakeService{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = server.Transcribe(t.Context(), &sttv1.TranscribeRequest{Audio: []byte("untrusted"), MediaType: "audio/mpeg"})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("ожидался Unauthenticated, получен %v", err)
	}
}

func TestServerBoundsConcurrentTranscriptions(t *testing.T) {
	server, err := New(fakeService{})
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < maximumConcurrentTranscriptions; index++ {
		if !server.acquire() {
			t.Fatal("доступная ёмкость отклонена")
		}
	}
	if server.acquire() {
		t.Fatal("запрос сверх bounded concurrency принят")
	}
	for index := 0; index < maximumConcurrentTranscriptions; index++ {
		server.release()
	}
}
