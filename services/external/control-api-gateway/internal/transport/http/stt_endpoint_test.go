package httptransport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type speechClientStub struct {
	sttv1.SpeechToTextServiceClient
	stream *speechStreamStub
	calls  int
	ctx    context.Context
	err    error
}

func (client *speechClientStub) Transcribe(ctx context.Context, _ ...grpc.CallOption) (sttv1.SpeechToTextService_TranscribeClient, error) {
	client.calls++
	client.ctx = ctx
	return client.stream, client.err
}

type speechStreamStub struct {
	grpc.ClientStream
	messages []*sttv1.TranscribeRequest
	response *sttv1.TranscribeResponse
	sendErr  error
	recvErr  error
}

func (stream *speechStreamStub) Send(message *sttv1.TranscribeRequest) error {
	if stream.sendErr != nil {
		return stream.sendErr
	}
	stream.messages = append(stream.messages, message)
	return nil
}

func (stream *speechStreamStub) CloseAndRecv() (*sttv1.TranscribeResponse, error) {
	return stream.response, stream.recvErr
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

	server.TranscribeSpeech(response, request, "prj_project01", generated.TranscribeSpeechParams{XAudioSize: int64(len("not multipart"))})

	if response.Code != http.StatusUnsupportedMediaType || client.calls != 0 || response.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("invalid envelope reached STT: status=%d calls=%d", response.Code, client.calls)
	}
}

func TestOrganizationSpeechSupportsBrowserFormatsAndCancelsStream(t *testing.T) {
	for _, mediaType := range []string{"audio/mp3", "audio/mpga", "audio/webm; codecs=opus", "audio/ogg; codecs=opus", "audio/mp4", "audio/x-m4a", "audio/flac", "audio/wav"} {
		t.Run(mediaType, func(t *testing.T) {
			body := &bytes.Buffer{}
			form := multipart.NewWriter(body)
			header := make(textproto.MIMEHeader)
			header.Set("Content-Disposition", `form-data; name="audio"; filename="recording"`)
			header.Set("Content-Type", mediaType)
			part, err := form.CreatePart(header)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = part.Write([]byte("audio"))
			if err := form.Close(); err != nil {
				t.Fatal(err)
			}
			client := &speechClientStub{stream: &speechStreamStub{sendErr: status.Error(codes.Unavailable, "unavailable")}}
			server := &Server{speech: client}
			request := httptest.NewRequest(http.MethodPost, "/api/v1/speech/transcriptions", body)
			request.Header.Set("Content-Type", form.FormDataContentType())
			request.Header.Set("X-Audio-Size", "5")
			request.Header.Set("X-CSRF-Token", "test-csrf")
			response := httptest.NewRecorder()
			generated.Handler(server).ServeHTTP(response, request)
			if response.Code != http.StatusServiceUnavailable || client.calls != 1 || client.ctx.Err() != context.Canceled {
				t.Fatalf("format or cancellation failed: status=%d calls=%d", response.Code, client.calls)
			}
		})
	}
}

func TestSpeechAvailabilityUsesProtectedStreamWithoutAudio(t *testing.T) {
	until := time.Now().Add(20 * time.Second)
	client := &speechClientStub{stream: &speechStreamStub{response: &sttv1.TranscribeResponse{Availability: &sttv1.CheckProtectedPathResponse{
		Ready: true, Stage: sttv1.ProtectedPathStage_PROTECTED_PATH_STAGE_READY, ValidUntil: timestamppb.New(until),
	}}}}
	server := &Server{speech: client}
	response := httptest.NewRecorder()
	server.writeBootstrapState(response, httptest.NewRequest(http.MethodGet, "/api/v1/bootstrap", nil), &controlplanev1.BootstrapState{
		SpeechTranscription: &controlplanev1.SpeechTranscriptionAvailability{Eligible: true},
	})
	var output struct {
		SpeechTranscription generated.SpeechTranscriptionAvailability `json:"speechTranscription"`
	}
	if json.Unmarshal(response.Body.Bytes(), &output) != nil || !output.SpeechTranscription.Available || output.SpeechTranscription.Reason != "READY" || output.SpeechTranscription.ValidUntil == nil || !output.SpeechTranscription.ValidUntil.Equal(until) {
		t.Fatalf("availability changed: %s", response.Body.String())
	}
	if len(client.stream.messages) != 1 || client.stream.messages[0].GetAvailabilityCheck() == nil || client.ctx.Err() != context.Canceled {
		t.Fatal("availability did not use a single bounded protected check")
	}
	if response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), "eligible") {
		t.Fatal("internal eligibility or cacheable state exposed")
	}
}

func TestSpeechAvailabilityFailsClosedWithoutBreakingBootstrap(t *testing.T) {
	for _, tc := range []struct {
		name     string
		owner    *controlplanev1.SpeechTranscriptionAvailability
		response *sttv1.TranscribeResponse
		err      error
		calls    int
		reason   string
	}{
		{"no-policy", nil, nil, nil, 0, "STT_SERVICE_UNAVAILABLE"},
		{"denied", &controlplanev1.SpeechTranscriptionAvailability{Available: true, Reason: "STT_PERMISSION_DENIED"}, nil, nil, 0, "STT_PERMISSION_DENIED"},
		{"unknown-reason", &controlplanev1.SpeechTranscriptionAvailability{Reason: "private-provider-detail"}, nil, nil, 0, "STT_SERVICE_UNAVAILABLE"},
		{"transport-denial", &controlplanev1.SpeechTranscriptionAvailability{Eligible: true}, nil, status.Error(codes.PermissionDenied, "denied"), 1, "STT_PERMISSION_DENIED"},
		{"no-receipt", &controlplanev1.SpeechTranscriptionAvailability{Eligible: true}, &sttv1.TranscribeResponse{}, nil, 1, "STT_SERVICE_UNAVAILABLE"},
		{"stale", &controlplanev1.SpeechTranscriptionAvailability{Eligible: true}, &sttv1.TranscribeResponse{Availability: &sttv1.CheckProtectedPathResponse{Ready: true, Stage: sttv1.ProtectedPathStage_PROTECTED_PATH_STAGE_READY, ValidUntil: timestamppb.New(time.Now().Add(-time.Second))}}, nil, 1, "STT_SERVICE_UNAVAILABLE"},
		{"unbounded", &controlplanev1.SpeechTranscriptionAvailability{Eligible: true}, &sttv1.TranscribeResponse{Availability: &sttv1.CheckProtectedPathResponse{Ready: true, Stage: sttv1.ProtectedPathStage_PROTECTED_PATH_STAGE_READY, ValidUntil: timestamppb.New(time.Now().Add(time.Hour))}}, nil, 1, "STT_SERVICE_UNAVAILABLE"},
		{"credential", &controlplanev1.SpeechTranscriptionAvailability{Eligible: true}, &sttv1.TranscribeResponse{Availability: &sttv1.CheckProtectedPathResponse{Stage: sttv1.ProtectedPathStage_PROTECTED_PATH_STAGE_CREDENTIAL}}, nil, 1, "STT_CREDENTIAL_UNAVAILABLE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &speechClientStub{stream: &speechStreamStub{response: tc.response}, err: tc.err}
			server := &Server{speech: client}
			result := server.speechAvailability(context.Background(), tc.owner)
			if result.Available || string(result.Reason) != tc.reason || result.ValidUntil != nil || client.calls != tc.calls {
				t.Fatalf("unsafe availability: %+v calls=%d", result, client.calls)
			}
		})
	}
}

func TestSpeechRejectsUnsupportedFormatsAndMalformedLengthBeforeRPC(t *testing.T) {
	for _, tc := range []struct {
		mediaType     string
		contentLength int64
		want          int
	}{
		{"audio/aac", -1, http.StatusUnsupportedMediaType},
		{"text/plain", -1, http.StatusUnsupportedMediaType},
		{"audio/webm; codecs=unknown", -1, http.StatusUnsupportedMediaType},
		{"audio/ogg; name=private", -1, http.StatusUnsupportedMediaType},
		{"audio/mp3", maximumAudioBytes + maximumMultipartOverhead + 1, http.StatusRequestEntityTooLarge},
		{"audio/mp3", 4, http.StatusBadRequest},
	} {
		body := &bytes.Buffer{}
		form := multipart.NewWriter(body)
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", `form-data; name="audio"; filename="recording"`)
		header.Set("Content-Type", tc.mediaType)
		part, err := form.CreatePart(header)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write([]byte("audio"))
		if err := form.Close(); err != nil {
			t.Fatal(err)
		}
		client := &speechClientStub{}
		server := &Server{speech: client}
		request := httptest.NewRequest(http.MethodPost, "/api/v1/speech/transcriptions", body)
		request.Header.Set("Content-Type", form.FormDataContentType())
		request.ContentLength = tc.contentLength
		response := httptest.NewRecorder()
		server.TranscribeOrganizationSpeech(response, request, generated.TranscribeOrganizationSpeechParams{XAudioSize: 5})
		if response.Code != tc.want || client.calls != 0 {
			t.Fatalf("invalid envelope reached RPC: status=%d calls=%d", response.Code, client.calls)
		}
	}
}

func TestUnavailableSpeechBootstrapHasExplicitFalseAndNoError(t *testing.T) {
	server := &Server{}
	response := httptest.NewRecorder()
	server.writeBootstrapState(response, httptest.NewRequest(http.MethodGet, "/api/v1/bootstrap", nil), &controlplanev1.BootstrapState{SpeechTranscription: &controlplanev1.SpeechTranscriptionAvailability{Eligible: true, Available: true}})
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"available":false`) || !strings.Contains(response.Body.String(), `"reason":"STT_SERVICE_UNAVAILABLE"`) {
		t.Fatalf("unavailable STT broke bootstrap: %s", response.Body.String())
	}
}

func TestSpeechEarlyStreamClosurePreservesServerDenial(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want int
	}{
		{status.Error(codes.PermissionDenied, "denied"), http.StatusForbidden},
		{status.Error(codes.ResourceExhausted, "rate limited"), http.StatusTooManyRequests},
		{nil, http.StatusBadGateway},
	} {
		response := httptest.NewRecorder()
		writeSpeechSendProblem(response, &speechStreamStub{recvErr: tc.err}, io.EOF)
		if response.Code != tc.want {
			t.Fatalf("early closure lost server status: got=%d want=%d", response.Code, tc.want)
		}
	}
}
