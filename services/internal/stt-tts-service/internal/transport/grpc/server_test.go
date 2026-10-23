package grpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"google.golang.org/protobuf/proto"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/transcriptiontest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// fakeStream supplies queued protobuf frames and captures responses, optionally blocking end-of-input until an explicit test release rather than context cancellation.
// RU: fakeStream выдаёт подготовленные protobuf кадры и сохраняет ответы, при необходимости блокируя конец ввода до явного тестового освобождения, а не отмены контекста.
type fakeStream struct {
	ctx      context.Context
	messages []*sttv1.TranscribeRequest
	index    int
	response *sttv1.TranscribeResponse
	block    <-chan struct{}
}

// Context returns the fixture's chosen request lifetime for the real stream wrapper to preserve or narrow.
// RU: Context возвращает выбранный оснасткой срок запроса для сохранения или сужения настоящей stream-обёрткой.
func (stream *fakeStream) Context() context.Context { return stream.ctx }

// SetHeader accepts synthetic response headers without storing or sending them.
// RU: SetHeader принимает синтетические заголовки ответа без сохранения или отправки.
func (*fakeStream) SetHeader(metadata.MD) error { return nil }

// SendHeader accepts synthetic header sends without creating a network effect.
// RU: SendHeader принимает синтетическую отправку заголовков без сетевого эффекта.
func (*fakeStream) SendHeader(metadata.MD) error { return nil }

// SetTrailer deliberately ignores response trailers in this frame/admission fixture.
// RU: SetTrailer намеренно игнорирует trailer ответа в оснастке framing/admission.
func (*fakeStream) SetTrailer(metadata.MD) {}

// SendMsg captures only a typed transcription response and otherwise leaves fixture state unchanged.
// RU: SendMsg сохраняет только типизированный ответ распознавания, иначе не меняя состояние оснастки.
func (stream *fakeStream) SendMsg(message any) error {
	response, ok := message.(*sttv1.TranscribeResponse)
	if ok {
		stream.response = response
	}
	return nil
}

// RecvMsg resets and copies the next fixture frame into a typed request, returning EOF after an optional explicit unblock at the end.
// RU: RecvMsg сбрасывает и копирует следующий кадр оснастки в типизированный запрос, возвращая EOF после необязательной явной разблокировки конца ввода.
func (stream *fakeStream) RecvMsg(message any) error {
	if stream.index >= len(stream.messages) {
		if stream.block != nil {
			<-stream.block
		}
		return io.EOF
	}
	request, ok := message.(*sttv1.TranscribeRequest)
	if !ok {
		return errors.New("unexpected receive type")
	}
	proto.Reset(request)
	proto.Merge(request, stream.messages[stream.index])
	stream.index++
	return nil
}

// Recv exposes the same synthetic frame sequence through the generated streaming server's typed receive signature.
// RU: Recv предоставляет ту же синтетическую последовательность кадров через типизированную сигнатуру приёма generated stream server.
func (stream *fakeStream) Recv() (*sttv1.TranscribeRequest, error) {
	message := &sttv1.TranscribeRequest{}
	if err := stream.RecvMsg(message); err != nil {
		return nil, err
	}
	return message, nil
}

// SendAndClose stores the final fixture response without closing a real transport or invoking a provider.
// RU: SendAndClose сохраняет итоговый ответ оснастки без закрытия настоящего транспорта или вызова провайдера.
func (stream *fakeStream) SendAndClose(response *sttv1.TranscribeResponse) error {
	stream.response = response
	return nil
}

// TestTranscribeRejectsMissingVerifiedContext proves direct handler entry cannot consume work without accepted authorization context.
// RU: TestTranscribeRejectsMissingVerifiedContext доказывает невозможность обработки при прямом входе в handler без принятого context авторизации.
func TestTranscribeRejectsMissingVerifiedContext(t *testing.T) {
	server := newTestServer(t)
	err := server.Transcribe(&fakeStream{ctx: t.Context()})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code=%s err=%v", status.Code(err), err)
	}
}

// TestServerBoundsConcurrentTranscriptions fills the declared stream/byte caps, rejects a third reservation and checks the budget against the actual Pod memory manifest.
// RU: TestServerBoundsConcurrentTranscriptions заполняет заявленные stream/byte пределы, отклоняет третий резерв и сверяет бюджет с настоящим memory manifest Pod.
func TestServerBoundsConcurrentTranscriptions(t *testing.T) {
	admission := &byteAdmission{}
	first, ok := admission.acquireStream()
	if !ok || !first.reserveBytes(value.MaximumAbsoluteBytes) {
		t.Fatal("первый максимальный запрос должен укладываться в byte budget")
	}
	second, ok := admission.acquireStream()
	if !ok || !second.reserveBytes(value.MaximumAbsoluteBytes) {
		t.Fatal("второй максимальный запрос должен укладываться в byte budget")
	}
	streams, bytes := admission.snapshot()
	if third, acquired := admission.acquireStream(); acquired || third != nil || bytes != value.MaximumInflightBytes || streams != value.MaximumConcurrentStreams {
		t.Fatal("запрос сверх concurrent byte/stream budget принят")
	}
	first.release()
	second.release()
	_, file, _, _ := runtime.Caller(0)
	manifest, err := os.ReadFile(filepath.Clean(filepath.Join(filepath.Dir(file), "../../../../../../deploy/k8s/base/stt-tts-service/deployment.yaml")))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), "limits: {cpu: \"2\", memory: 256Mi}") || value.MaximumInflightBytes >= 256<<20 {
		t.Fatal("code memory budget не согласован с Pod memory limit")
	}
}

// TestStreamAdmissionReservesBeforeFirstMessageAndReleasesOnTimeout blocks the first receive and checks early slot reservation, bounded timeout and released counters.
// RU: TestStreamAdmissionReservesBeforeFirstMessageAndReleasesOnTimeout блокирует первое чтение и проверяет ранний резерв slot, ограниченный timeout и освобождённые счётчики.
func TestStreamAdmissionReservesBeforeFirstMessageAndReleasesOnTimeout(t *testing.T) {
	server := newTestServer(t)
	server.requestTimeout = 40 * time.Millisecond
	server.principal = func(context.Context, string) (value.Principal, error) {
		return value.Principal{ExpiresAt: time.Now().Add(time.Second)}, nil
	}
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	entered := make(chan struct{})
	result := make(chan error, 1)
	stream := &fakeStream{ctx: t.Context(), block: release}
	go func() {
		result <- server.StreamServerInterceptor()(nil, stream, &grpc.StreamServerInfo{FullMethod: sttv1.SpeechToTextService_Transcribe_FullMethodName}, func(_ any, admitted grpc.ServerStream) error {
			close(entered)
			var message sttv1.TranscribeRequest
			return admitted.RecvMsg(&message)
		})
	}()
	<-entered
	streams, _ := server.admission.snapshot()
	if streams != 1 {
		t.Fatalf("slot до первого Recv не зарезервирован: %d", streams)
	}
	if err := <-result; status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("code=%s err=%v", status.Code(err), err)
	}
	streams, reservedBytes := server.admission.snapshot()
	if streams != 0 || reservedBytes != 0 {
		t.Fatalf("slot после timeout не освобождён: streams=%d bytes=%d", streams, reservedBytes)
	}
}

// TestStreamAdmissionCapsStalledStreamsBeforeHandler fills active stalled slots and proves the next stream is rejected before its handler, then joins released fixtures.
// RU: TestStreamAdmissionCapsStalledStreamsBeforeHandler заполняет активные зависшие slot и доказывает отказ следующему потоку до handler, затем ожидает освобождённые оснастки.
func TestStreamAdmissionCapsStalledStreamsBeforeHandler(t *testing.T) {
	server := newTestServer(t)
	server.principal = func(context.Context, string) (value.Principal, error) {
		return value.Principal{ExpiresAt: time.Now().Add(2 * time.Second)}, nil
	}
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseStreams := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseStreams)
	entered := make(chan struct{}, value.MaximumConcurrentStreams)
	results := make(chan error, value.MaximumConcurrentStreams)
	for range value.MaximumConcurrentStreams {
		stream := &fakeStream{ctx: t.Context(), block: release}
		go func() {
			results <- server.StreamServerInterceptor()(nil, stream, &grpc.StreamServerInfo{FullMethod: sttv1.SpeechToTextService_Transcribe_FullMethodName}, func(_ any, admitted grpc.ServerStream) error {
				entered <- struct{}{}
				var message sttv1.TranscribeRequest
				return admitted.RecvMsg(&message)
			})
		}()
	}
	for range value.MaximumConcurrentStreams {
		<-entered
	}
	thirdHandlerCalled := false
	err := server.StreamServerInterceptor()(nil, &fakeStream{ctx: t.Context()}, &grpc.StreamServerInfo{FullMethod: sttv1.SpeechToTextService_Transcribe_FullMethodName}, func(any, grpc.ServerStream) error {
		thirdHandlerCalled = true
		return nil
	})
	if status.Code(err) != codes.ResourceExhausted || thirdHandlerCalled {
		t.Fatalf("stream cap не сработал до handler: called=%v code=%s", thirdHandlerCalled, status.Code(err))
	}
	releaseStreams()
	for range value.MaximumConcurrentStreams {
		if result := <-results; !errors.Is(result, io.EOF) {
			t.Fatalf("stalled stream result=%v", result)
		}
	}
	streams, bytes := server.admission.snapshot()
	if streams != 0 || bytes != 0 {
		t.Fatalf("slots после завершения не освобождены: streams=%d bytes=%d", streams, bytes)
	}
}

// TestStreamAdmissionReleasesBytesWhenStalledAfterMetadata reserves declared bytes after metadata and verifies cancellation returns both stream and byte counts.
// RU: TestStreamAdmissionReleasesBytesWhenStalledAfterMetadata резервирует заявленные байты после metadata и проверяет возврат счётчиков потока и байтов по отмене.
func TestStreamAdmissionReleasesBytesWhenStalledAfterMetadata(t *testing.T) {
	server := newTestServer(t)
	server.requestTimeout = 40 * time.Millisecond
	server.principal = func(context.Context, string) (value.Principal, error) {
		return value.Principal{ExpiresAt: time.Now().Add(time.Second)}, nil
	}
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	stream := &fakeStream{ctx: t.Context(), messages: []*sttv1.TranscribeRequest{{Body: &sttv1.TranscribeRequest_Metadata{Metadata: &sttv1.TranscribeMetadata{SizeBytes: 1024}}}}, block: release}
	err := server.StreamServerInterceptor()(nil, stream, &grpc.StreamServerInfo{FullMethod: sttv1.SpeechToTextService_Transcribe_FullMethodName}, func(_ any, admitted grpc.ServerStream) error {
		var metadata sttv1.TranscribeRequest
		if receiveErr := admitted.RecvMsg(&metadata); receiveErr != nil {
			return receiveErr
		}
		reservation := admitted.Context().Value(streamReservationContextKey{}).(*streamReservation)
		if !reservation.reserveBytes(int64(metadata.GetMetadata().GetSizeBytes())) {
			return errors.New("reserve metadata bytes")
		}
		var chunk sttv1.TranscribeRequest
		return admitted.RecvMsg(&chunk)
	})
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("code=%s err=%v", status.Code(err), err)
	}
	streams, bytes := server.admission.snapshot()
	if streams != 0 || bytes != 0 {
		t.Fatalf("reservation после timeout не освобождена: streams=%d bytes=%d", streams, bytes)
	}
}

// TestStreamAdmissionDeadlineIsCappedByAuthorityExpiry chooses an authority lifetime shorter than the request budget and rejects a stalled read at that earlier deadline.
// RU: TestStreamAdmissionDeadlineIsCappedByAuthorityExpiry выбирает срок полномочий короче бюджета запроса и отклоняет зависшее чтение по этому раннему deadline.
func TestStreamAdmissionDeadlineIsCappedByAuthorityExpiry(t *testing.T) {
	server := newTestServer(t)
	server.requestTimeout = time.Second
	expiresAt := time.Now().Add(50 * time.Millisecond)
	server.principal = func(context.Context, string) (value.Principal, error) {
		return value.Principal{ExpiresAt: expiresAt}, nil
	}
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	stream := &fakeStream{ctx: t.Context(), block: release}
	err := server.StreamServerInterceptor()(nil, stream, &grpc.StreamServerInfo{FullMethod: sttv1.SpeechToTextService_Transcribe_FullMethodName}, func(_ any, admitted grpc.ServerStream) error {
		deadline, ok := admitted.Context().Deadline()
		if !ok || deadline.After(expiresAt) {
			t.Fatalf("deadline=%v authority expiry=%v", deadline, expiresAt)
		}
		var message sttv1.TranscribeRequest
		return admitted.RecvMsg(&message)
	})
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("code=%s err=%v", status.Code(err), err)
	}
}

// TestReceiveAudioRequiresExactCommitAndNoTrailingMessage rejects digest drift, size drift and frames after commit rather than accepting partial or extended audio.
// RU: TestReceiveAudioRequiresExactCommitAndNoTrailingMessage отклоняет иной дайджест, размер и кадры после commit вместо принятия частичного или расширенного аудио.
func TestReceiveAudioRequiresExactCommitAndNoTrailingMessage(t *testing.T) {
	audio := []byte("bounded-audio")
	digest := sha256.Sum256(audio)
	valid := []*sttv1.TranscribeRequest{
		{Body: &sttv1.TranscribeRequest_Chunk{Chunk: audio}},
		{Body: &sttv1.TranscribeRequest_Commit{Commit: &sttv1.TranscribeCommit{SizeBytes: uint64(len(audio)), Sha256: hex.EncodeToString(digest[:])}}},
	}
	for _, test := range []struct {
		name   string
		mutate func([]*sttv1.TranscribeRequest)
	}{
		{name: "digest mismatch", mutate: func(messages []*sttv1.TranscribeRequest) {
			messages[1].GetCommit().Sha256 = strings.Repeat("0", 64)
		}},
		{name: "size mismatch", mutate: func(messages []*sttv1.TranscribeRequest) { messages[1].GetCommit().SizeBytes++ }},
		{name: "trailing message", mutate: func(messages []*sttv1.TranscribeRequest) {
			messages = append(messages, &sttv1.TranscribeRequest{Body: &sttv1.TranscribeRequest_Chunk{Chunk: []byte("x")}})
			valid = messages
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			messages := cloneMessages(valid[:2])
			if test.name == "trailing message" {
				messages = append(messages, &sttv1.TranscribeRequest{Body: &sttv1.TranscribeRequest_Chunk{Chunk: []byte("x")}})
			} else {
				test.mutate(messages)
			}
			stream := &fakeStream{ctx: t.Context(), messages: messages}
			if _, _, err := receiveAudio(stream, &strings.Builder{}, int64(len(audio))); err == nil {
				t.Fatal("неверный commit принят")
			}
		})
	}
}

// TestReceiveAudioAcceptsExactCommit verifies byte-for-byte output, total size and declared SHA-256 on an exactly terminated fixture stream.
// RU: TestReceiveAudioAcceptsExactCommit проверяет побайтовый выход, общий размер и заявленный SHA-256 точно завершённого потока оснастки.
func TestReceiveAudioAcceptsExactCommit(t *testing.T) {
	audio := []byte("bounded-audio")
	digest := sha256.Sum256(audio)
	stream := &fakeStream{ctx: t.Context(), messages: []*sttv1.TranscribeRequest{
		{Body: &sttv1.TranscribeRequest_Chunk{Chunk: audio}},
		{Body: &sttv1.TranscribeRequest_Commit{Commit: &sttv1.TranscribeCommit{
			SizeBytes: uint64(len(audio)), Sha256: hex.EncodeToString(digest[:]),
		}}},
	}}
	output := &strings.Builder{}
	written, declared, err := receiveAudio(stream, output, int64(len(audio)))
	if err != nil || written != int64(len(audio)) || declared != hex.EncodeToString(digest[:]) || output.String() != string(audio) {
		t.Fatalf("exact commit result: written=%d digest=%q err=%v", written, declared, err)
	}
}

// cloneMessages copies only the chunk/commit frame shapes used by these fixtures, including independent audio byte slices for mutation tests.
// RU: cloneMessages копирует только используемые оснасткой формы chunk/commit, включая независимые аудиобайты для тестовых изменений.
func cloneMessages(input []*sttv1.TranscribeRequest) []*sttv1.TranscribeRequest {
	result := make([]*sttv1.TranscribeRequest, len(input))
	for index, message := range input {
		if chunk := message.GetChunk(); chunk != nil {
			result[index] = &sttv1.TranscribeRequest{Body: &sttv1.TranscribeRequest_Chunk{Chunk: append([]byte(nil), chunk...)}}
			continue
		}
		commit := message.GetCommit()
		result[index] = &sttv1.TranscribeRequest{Body: &sttv1.TranscribeRequest_Commit{Commit: &sttv1.TranscribeCommit{SizeBytes: commit.GetSizeBytes(), Sha256: commit.GetSha256()}}}
	}
	return result
}

// newTestServer constructs a locally ready server with private spool, shared stateless test service and a one-second request budget.
// RU: newTestServer создаёт локально готовый сервер с отдельным spool, общей stateless-заглушкой и бюджетом запроса одна секунда.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	readiness := serviceruntime.NewReadiness()
	readiness.Set(true, "ready")
	server, err := New(transcriptiontest.Service{}, t.TempDir(), readiness, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return server
}
