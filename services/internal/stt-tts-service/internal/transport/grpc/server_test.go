package grpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	transcriptionservice "github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/service/transcription"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type fakeService struct{}

func (fakeService) Transcribe(context.Context, transcriptionservice.Input) (value.TranscriptionResult, error) {
	return value.TranscriptionResult{}, nil
}
func (fakeService) CheckLocal(context.Context) error         { return nil }
func (fakeService) CheckProtectedPath(context.Context) error { return errors.New("pending") }

type fakeStream struct {
	ctx      context.Context
	messages []*sttv1.TranscribeRequest
	index    int
	response *sttv1.TranscribeResponse
}

func (stream *fakeStream) Context() context.Context { return stream.ctx }
func (*fakeStream) SetHeader(metadata.MD) error     { return nil }
func (*fakeStream) SendHeader(metadata.MD) error    { return nil }
func (*fakeStream) SetTrailer(metadata.MD)          {}
func (*fakeStream) SendMsg(any) error               { return nil }
func (*fakeStream) RecvMsg(any) error               { return nil }
func (stream *fakeStream) Recv() (*sttv1.TranscribeRequest, error) {
	if stream.index >= len(stream.messages) {
		return nil, io.EOF
	}
	message := stream.messages[stream.index]
	stream.index++
	return message, nil
}
func (stream *fakeStream) SendAndClose(response *sttv1.TranscribeResponse) error {
	stream.response = response
	return nil
}

func TestTranscribeRejectsMissingVerifiedContext(t *testing.T) {
	server, err := New(fakeService{}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	err = server.Transcribe(&fakeStream{ctx: t.Context()})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code=%s err=%v", status.Code(err), err)
	}
}

func TestServerBoundsConcurrentTranscriptions(t *testing.T) {
	admission := &byteAdmission{}
	if !admission.acquire(value.MaximumAbsoluteBytes) {
		t.Fatal("первый максимальный запрос должен укладываться в byte budget")
	}
	if !admission.acquire(value.MaximumAbsoluteBytes) {
		t.Fatal("второй максимальный запрос должен укладываться в byte budget")
	}
	if admission.acquire(1) || admission.bytes != value.MaximumInflightBytes || admission.streams != value.MaximumConcurrentStreams {
		t.Fatal("запрос сверх concurrent byte/stream budget принят")
	}
	admission.release(value.MaximumAbsoluteBytes)
	admission.release(value.MaximumAbsoluteBytes)
	_, file, _, _ := runtime.Caller(0)
	manifest, err := os.ReadFile(filepath.Clean(filepath.Join(filepath.Dir(file), "../../../../../../deploy/k8s/base/stt-tts-service/deployment.yaml")))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), "limits: {cpu: \"2\", memory: 256Mi}") || value.MaximumInflightBytes >= 256<<20 {
		t.Fatal("code memory budget не согласован с Pod memory limit")
	}
}

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
