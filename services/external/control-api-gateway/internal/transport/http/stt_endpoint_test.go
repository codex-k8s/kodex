package httptransport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc"
)

type speechClientStub struct {
	stream *speechStreamStub
	calls  int
}

func (client *speechClientStub) Transcribe(context.Context, ...grpc.CallOption) (sttv1.SpeechToTextService_TranscribeClient, error) {
	client.calls++
	return client.stream, nil
}

type speechStreamStub struct {
	grpc.ClientStream
	messages []*sttv1.TranscribeRequest
	response *sttv1.TranscribeResponse
	sendErr  error
}

func (stream *speechStreamStub) Send(message *sttv1.TranscribeRequest) error {
	if stream.sendErr != nil {
		return stream.sendErr
	}
	stream.messages = append(stream.messages, message)
	return nil
}

func (stream *speechStreamStub) CloseAndRecv() (*sttv1.TranscribeResponse, error) {
	return stream.response, nil
}

func TestForwardAudioPartUsesBoundedBackpressure(t *testing.T) {
	raw := bytes.Repeat([]byte("a"), maximumAudioChunkBytes*2+7)
	chunkSizes := make([]int, 0)
	received, digest, err := forwardAudioPart(bytes.NewReader(raw), int64(len(raw)), func(chunk []byte) error {
		chunkSizes = append(chunkSizes, len(chunk))
		if len(chunk) > maximumAudioChunkBytes {
			t.Fatalf("unbounded chunk: %d", len(chunk))
		}
		return nil
	})
	if err != nil || received != int64(len(raw)) || len(chunkSizes) != 3 {
		t.Fatalf("forward audio = bytes %d chunks %v error %v", received, chunkSizes, err)
	}
	want := sha256.Sum256(raw)
	if digest != hex.EncodeToString(want[:]) {
		t.Fatalf("digest = %s", digest)
	}

	sentinel := errors.New("downstream flow control failed")
	reads := &countingReader{reader: bytes.NewReader(raw)}
	_, _, err = forwardAudioPart(reads, int64(len(raw)), func([]byte) error { return sentinel })
	if !errors.Is(err, sentinel) || reads.bytes > maximumAudioChunkBytes {
		t.Fatalf("backpressure failure read ahead: bytes=%d error=%v", reads.bytes, err)
	}
}

type countingReader struct {
	reader *bytes.Reader
	bytes  int
}

func (reader *countingReader) Read(output []byte) (int, error) {
	count, err := reader.reader.Read(output)
	reader.bytes += count
	return count, err
}

func TestTranscribeSpeechStreamsMultipartAndReturnsSafeReceipt(t *testing.T) {
	audio := bytes.Repeat([]byte("audio"), 20_000)
	body := &bytes.Buffer{}
	form := multipart.NewWriter(body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="audio"; filename="recording.mp3"`)
	header.Set("Content-Type", "audio/mpeg")
	part, err := form.CreatePart(header)
	if err != nil {
		t.Fatalf("create audio part: %v", err)
	}
	_, _ = part.Write(audio)
	if err := form.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	stream := &speechStreamStub{response: &sttv1.TranscribeResponse{
		Text: "recognized text",
		Receipt: &sttv1.TranscriptionReceipt{
			RequestId: "00000000-0000-4000-8000-000000000001", CorrelationId: "00000000-0000-4000-8000-000000000002",
			ActorId: "must-not-leak", TenantId: "must-not-leak", ProjectId: "must-not-leak", ProviderAccountRef: "must-not-leak",
			AuthoritySourceRevision: 7, ConfigRevision: 3, Model: "whisper-1", Language: "ru",
			CompletedStage: sttv1.TranscriptionStage_TRANSCRIPTION_STAGE_PROVIDER_COMPLETED,
		},
	}}
	client := &speechClientStub{stream: stream}
	server := &Server{speech: client}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/projects/prj_project01/speech/transcriptions", body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	response := httptest.NewRecorder()

	server.TranscribeSpeech(response, request, "prj_project01", generated.TranscribeSpeechParams{XAudioSize: int64(len(audio))})

	if response.Code != http.StatusOK || client.calls != 1 || len(stream.messages) < 3 {
		t.Fatalf("transcription = status %d calls %d messages %d body %s", response.Code, client.calls, len(stream.messages), response.Body.String())
	}
	metadata := stream.messages[0].GetMetadata()
	commit := stream.messages[len(stream.messages)-1].GetCommit()
	if metadata.GetMediaType() != "audio/mpeg" || metadata.GetSizeBytes() != uint64(len(audio)) || commit.GetSizeBytes() != uint64(len(audio)) {
		t.Fatalf("stream envelope is invalid: metadata=%v commit=%v", metadata, commit)
	}
	for _, forbidden := range []string{"must-not-leak", "providerAccountRef", "actorId", "tenantId", "projectId"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("unsafe receipt field leaked: %s", forbidden)
		}
	}
}

func TestTranscribeSpeechRejectsEnvelopeBeforeRPC(t *testing.T) {
	client := &speechClientStub{stream: &speechStreamStub{}}
	server := &Server{speech: client}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/projects/prj_project01/speech/transcriptions", strings.NewReader("not multipart"))
	request.Header.Set("Content-Type", "application/octet-stream")
	response := httptest.NewRecorder()

	server.TranscribeSpeech(response, request, "prj_project01", generated.TranscribeSpeechParams{XAudioSize: 128})

	if response.Code != http.StatusUnsupportedMediaType || client.calls != 0 || response.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("invalid envelope reached STT: status=%d calls=%d", response.Code, client.calls)
	}
}
