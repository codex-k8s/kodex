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

// speechClientStub records transcription call count and context and returns a configured synthetic
// stream/error while embedding unused generated client methods. It contacts no STT service.
// speechClientStub сохраняет число вызовов транскрипции и контекст и возвращает заданные
// синтетические stream/error, встраивая неиспользуемые generated методы. STT сервис не вызывается.
type speechClientStub struct {
	sttv1.SpeechToTextServiceClient
	stream *speechStreamStub
	calls  int
	ctx    context.Context
	err    error
}

// Transcribe records the caller context and increments the stub invocation count before returning
// the configured stream and startup error. Context cancellation can then be asserted by the test.
// Transcribe сохраняет контекст caller и увеличивает число вызовов stub перед возвратом заданных
// stream и startup ошибки. Тест затем может проверить отмену контекста.
func (client *speechClientStub) Transcribe(ctx context.Context, _ ...grpc.CallOption) (sttv1.SpeechToTextService_TranscribeClient, error) {
	client.calls++
	client.ctx = ctx
	return client.stream, client.err
}

// speechStreamStub stores sent typed messages and configured response/send/receive errors,
// allowing transport envelope and early server-denial behavior to be tested without a provider.
// speechStreamStub хранит отправленные typed сообщения и заданные response/send/receive ошибки для
// проверки transport envelope и раннего отказа сервера без провайдера.
type speechStreamStub struct {
	grpc.ClientStream
	messages []*sttv1.TranscribeRequest
	response *sttv1.TranscribeResponse
	sendErr  error
	recvErr  error
}

// Send returns the configured send error before recording a message, otherwise appends the exact
// request pointer for later envelope/chunk assertions.
// Send возвращает заданную send ошибку до сохранения сообщения, иначе добавляет точный указатель
// request для последующих проверок envelope/chunks.
func (stream *speechStreamStub) Send(message *sttv1.TranscribeRequest) error {
	if stream.sendErr != nil {
		return stream.sendErr
	}
	stream.messages = append(stream.messages, message)
	return nil
}

// CloseAndRecv returns the configured terminal response and receive error without inventing a
// successful receipt or performing stream I/O.
// CloseAndRecv возвращает заданные terminal response и receive ошибку без выдуманного успешного
// receipt и без stream I/O.
func (stream *speechStreamStub) CloseAndRecv() (*sttv1.TranscribeResponse, error) {
	return stream.response, stream.recvErr
}

// TestForwardAudioPartUsesBoundedBackpressure checks bounded chunk sizes and exact digest/count,
// then injects a downstream send failure and proves forwarding does not read beyond the first
// chunk after backpressure failure.
// TestForwardAudioPartUsesBoundedBackpressure проверяет ограниченные chunks и точные digest/count,
// затем задаёт отказ downstream send и доказывает отсутствие чтения дальше первого chunk после
// backpressure ошибки.
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

// countingReader wraps a byte reader with an observed-byte counter for detecting read-ahead after
// a failed send, without changing the underlying read semantics.
// countingReader оборачивает byte reader счётчиком наблюдённых байтов для обнаружения read-ahead
// после отказа send без изменения исходной семантики чтения.
type countingReader struct {
	reader *bytes.Reader
	bytes  int
}

// Read forwards the underlying reader result and increments the observed count by returned bytes,
// including reads that return an error alongside data.
// Read передаёт результат исходного reader и увеличивает наблюдённый count на полученные байты,
// включая чтение с данными вместе с ошибкой.
func (reader *countingReader) Read(output []byte) (int, error) {
	count, err := reader.reader.Read(output)
	reader.bytes += count
	return count, err
}

// TestTranscribeSpeechStreamsMultipartAndReturnsSafeReceipt checks a multipart project upload
// produces metadata/chunks/commit with the exact declared size and returns transcript without
// private actor/tenant/project/provider receipt fields.
// TestTranscribeSpeechStreamsMultipartAndReturnsSafeReceipt проверяет передачу multipart загрузки
// проекта как metadata/chunks/commit с точным объявленным размером и выдачу текста без приватных
// actor/tenant/project/provider полей receipt.
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

// TestTranscribeSpeechRejectsEnvelopeBeforeRPC requires unsupported nonmultipart input to return a
// problem+json 415 without invoking the STT client, proving envelope rejection precedes downstream
// work.
// TestTranscribeSpeechRejectsEnvelopeBeforeRPC требует problem+json 415 для неподдерживаемого
// немultipart входа без вызова STT клиента, доказывая отказ envelope до downstream работы.
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

// TestOrganizationSpeechSupportsBrowserFormatsAndCancelsStream exercises supported browser audio
// MIME aliases/codecs through the generated organization route and proves an upstream send failure
// returns 503 while cancelling the stream context.
// TestOrganizationSpeechSupportsBrowserFormatsAndCancelsStream проверяет поддерживаемые browser
// audio MIME aliases/codecs через generated организационный маршрут и доказывает 503 при upstream
// send ошибке с отменой stream контекста.
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

// TestSpeechPreservesEmptySingularHintAndRejectsUploadPolicy keeps empty and explicit language
// hints intact, rejects invalid receipt language and caller-supplied model/timeout parts, and
// verifies rejected policy fields never reach commit and cancel the stream.
// TestSpeechPreservesEmptySingularHintAndRejectsUploadPolicy сохраняет пустой и явный language
// hint, отклоняет неверный язык receipt и переданные caller model/timeout части и проверяет
// отсутствие commit и отмену stream при отказе policy полям.
func TestSpeechPreservesEmptySingularHintAndRejectsUploadPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, language, extra string
		code                  int
	}{
		{"model-languages", "", "", 200},
		{"singular-hint", "ru", "", 200},
		{"invalid-hint", "detected-private-text", "", 502},
		{"caller-timeout", "", "providerTimeoutMilliseconds", 400},
		{"caller-model", "", "model", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &bytes.Buffer{}
			form := multipart.NewWriter(body)
			header := make(textproto.MIMEHeader)
			header.Set("Content-Disposition", `form-data; name="audio"; filename="recording"`)
			header.Set("Content-Type", "audio/mp3")
			part, err := form.CreatePart(header)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = part.Write([]byte("audio"))
			if tc.extra != "" {
				if err := form.WriteField(tc.extra, "60000"); err != nil {
					t.Fatal(err)
				}
			}
			if err := form.Close(); err != nil {
				t.Fatal(err)
			}
			client := &speechClientStub{stream: &speechStreamStub{response: &sttv1.TranscribeResponse{Text: "fixture transcript", Receipt: &sttv1.TranscriptionReceipt{
				RequestId: "00000000-0000-4000-8000-000000000001", CorrelationId: "00000000-0000-4000-8000-000000000002",
				AuthoritySourceRevision: 2, ConfigRevision: 3, Model: "gpt-transcribe", Language: tc.language,
				CompletedStage: sttv1.TranscriptionStage_TRANSCRIPTION_STAGE_PROVIDER_COMPLETED,
			}}}}
			request := httptest.NewRequest(http.MethodPost, "/api/v1/speech/transcriptions", body)
			request.Header.Set("Content-Type", form.FormDataContentType())
			request.Header.Set("X-Audio-Size", "5")
			request.Header.Set("X-CSRF-Token", "fixture-csrf")
			response := httptest.NewRecorder()
			generated.Handler(&Server{speech: client}).ServeHTTP(response, request)
			if response.Code != tc.code {
				t.Fatalf("status=%d", response.Code)
			}
			if tc.code == 200 {
				var result generated.SpeechTranscription
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Receipt.Language != tc.language {
					t.Fatal("singular hint was inferred or lost")
				}
			}
			if tc.extra != "" {
				for _, message := range client.stream.messages {
					if message.GetCommit() != nil {
						t.Fatal("caller policy reached transcription commit")
					}
				}
				if client.ctx.Err() != context.Canceled {
					t.Fatal("rejected upload left stream active")
				}
			}
		})
	}
}

// TestSpeechAvailabilityUsesProtectedStreamWithoutAudio checks bootstrap availability uses one
// protected availability-only stream message, preserves the bounded expiry, cancels the context
// and omits internal eligibility under no-store.
// TestSpeechAvailabilityUsesProtectedStreamWithoutAudio проверяет единственное защищённое
// availability-only stream сообщение для bootstrap, сохранение ограниченного expiry, отмену
// контекста и отсутствие внутреннего eligibility под no-store.
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

// TestSpeechAvailabilityFailsClosedWithoutBreakingBootstrap covers absent/denied owner policy,
// unknown reasons, transport denial, missing/stale/oversized lease and credential-stage failure,
// requiring unavailable results with closed reasons and expected RPC counts.
// TestSpeechAvailabilityFailsClosedWithoutBreakingBootstrap проверяет отсутствие/отказ policy
// владельца, неизвестные причины, transport denial, отсутствующий/истёкший/длинный lease и
// credential отказ, требуя unavailable с закрытыми причинами и ожидаемым числом RPC.
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

// TestSpeechRejectsUnsupportedFormatsAndMalformedLengthBeforeRPC rejects unsupported MIME/codec
// parameters and impossible/oversized content lengths with their expected HTTP statuses before any
// transcription RPC.
// TestSpeechRejectsUnsupportedFormatsAndMalformedLengthBeforeRPC отклоняет неподдерживаемые
// MIME/codec параметры и невозможные либо длинные content lengths с ожидаемыми HTTP статусами до
// любого RPC транскрипции.
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

// TestUnavailableSpeechBootstrapHasExplicitFalseAndNoError proves an absent attached STT client
// preserves successful bootstrap HTTP 200 while explicitly reporting available false and a safe
// unavailable reason.
// TestUnavailableSpeechBootstrapHasExplicitFalseAndNoError доказывает сохранение успешного
// bootstrap HTTP 200 при отсутствии STT клиента с явными available false и безопасной причиной
// недоступности.
func TestUnavailableSpeechBootstrapHasExplicitFalseAndNoError(t *testing.T) {
	server := &Server{}
	response := httptest.NewRecorder()
	server.writeBootstrapState(response, httptest.NewRequest(http.MethodGet, "/api/v1/bootstrap", nil), &controlplanev1.BootstrapState{SpeechTranscription: &controlplanev1.SpeechTranscriptionAvailability{Eligible: true, Available: true}})
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"available":false`) || !strings.Contains(response.Body.String(), `"reason":"STT_SERVICE_UNAVAILABLE"`) {
		t.Fatalf("unavailable STT broke bootstrap: %s", response.Body.String())
	}
}

// TestSpeechEarlyStreamClosurePreservesServerDenial checks Send EOF resolves the receive-side
// PermissionDenied/ResourceExhausted statuses and treats an unexpectedly successful close as
// invalid upstream 502.
// TestSpeechEarlyStreamClosurePreservesServerDenial проверяет разрешение Send EOF в receive-side
// статусы PermissionDenied/ResourceExhausted и трактовку неожиданно успешного закрытия как
// неверного upstream 502.
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
