package httptransport

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc"
)

// avatarCommandStub counts synthetic avatar upload stream openings and returns the configured
// local stream without contacting the platform.
// avatarCommandStub подсчитывает открытия синтетического потока загрузки аватара и возвращает
// заданный локальный поток без обращения к платформе.
type avatarCommandStub struct {
	controlplanev1.PlatformCommandServiceClient
	stream *avatarStreamStub
	calls  int
}

// UploadAgentAvatar records one upload RPC opening and returns the configured synthetic client
// stream without transferring real avatar data.
// UploadAgentAvatar записывает открытие одного RPC загрузки и возвращает заданный синтетический
// клиентский поток без передачи настоящих данных аватара.
func (client *avatarCommandStub) UploadAgentAvatar(context.Context, ...grpc.CallOption) (controlplanev1.PlatformCommandService_UploadAgentAvatarClient, error) {
	client.calls++
	return client.stream, nil
}

// avatarStreamStub collects outgoing synthetic avatar frames behind the generated gRPC upload
// stream interface.
// avatarStreamStub собирает исходящие синтетические кадры аватара за generated-интерфейсом
// gRPC-потока загрузки.
type avatarStreamStub struct {
	grpc.ClientStream
	messages []*controlplanev1.UploadAgentAvatarRequest
}

// Send appends the supplied avatar frame to the fixture message list for metadata and commit
// assertions.
// Send добавляет переданный кадр аватара в тестовый список сообщений для проверок метаданных и
// commit.
func (stream *avatarStreamStub) Send(message *controlplanev1.UploadAgentAvatarRequest) error {
	stream.messages = append(stream.messages, message)
	return nil
}

// CloseAndRecv returns a synthetic version-five agent with an artifact-backed avatar and public
// content path after the test upload closes.
// CloseAndRecv возвращает синтетического агента пятой версии с аватаром из артефакта и публичным
// путём содержимого после закрытия тестовой загрузки.
func (stream *avatarStreamStub) CloseAndRecv() (*controlplanev1.UploadAgentAvatarResponse, error) {
	return &controlplanev1.UploadAgentAvatarResponse{Agent: &controlplanev1.Agent{
		Ref: "agt_employee01", Version: 5,
		Avatar: &controlplanev1.AgentAvatar{Source: controlplanev1.AgentAvatar_SOURCE_ARTIFACT, ArtifactRef: "art_avatar01", ArtifactRevision: 1, ContentPath: "/api/v1/agents/agt_employee01/avatar/content"},
	}}, nil
}

// TestUploadAgentAvatarUsesAtomicStreamingRPC checks one upload RPC with metadata, content and
// commit frames, exact size/version pins and canonical public avatar-source enum.
// TestUploadAgentAvatarUsesAtomicStreamingRPC проверяет один RPC загрузки с кадрами метаданных,
// содержимого и commit, точными pins размера и версии и каноническим публичным enum источника
// аватара.
func TestUploadAgentAvatarUsesAtomicStreamingRPC(t *testing.T) {
	raw := bytes.Repeat([]byte("png"), 30_000)
	stream := &avatarStreamStub{}
	command := &avatarCommandStub{stream: stream}
	server := &Server{control: &controlplaneclient.Client{Command: command}}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/projects/prj_project01/agents/agt_employee01/avatar", bytes.NewReader(raw))
	request.Header.Set("Content-Type", "image/png")
	response := httptest.NewRecorder()

	server.UploadAgentAvatar(response, request, "prj_project01", "agt_employee01", generated.UploadAgentAvatarParams{
		IfMatch: `"4"`, IdempotencyKey: "avatar-request-01", XFileName: "avatar.png",
	})

	if response.Code != http.StatusOK || command.calls != 1 || len(stream.messages) < 3 {
		t.Fatalf("avatar upload = status %d calls %d messages %d body %s", response.Code, command.calls, len(stream.messages), response.Body.String())
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(`"source":"ARTIFACT"`)) || bytes.Contains(response.Body.Bytes(), []byte("SOURCE_ARTIFACT")) {
		t.Fatalf("avatar source violates OpenAPI: %s", response.Body.String())
	}
	metadata := stream.messages[0].GetMetadata()
	commit := stream.messages[len(stream.messages)-1].GetCommit()
	if metadata.GetProjectRef() != "prj_project01" || metadata.GetAgentRef() != "agt_employee01" || metadata.GetSizeBytes() != int64(len(raw)) ||
		metadata.GetMutation().GetExpectedVersion() != 4 || commit.GetSizeBytes() != int64(len(raw)) || commit.GetSha256() == "" {
		t.Fatalf("atomic avatar envelope is invalid: metadata=%v commit=%v", metadata, commit)
	}
}

// TestUploadAgentAvatarRejectsOversizeBeforeRPC checks that an advertised body length beyond the
// avatar limit returns payload too large before opening an upload RPC.
// TestUploadAgentAvatarRejectsOversizeBeforeRPC проверяет ответ payload too large при объявленной
// длине тела сверх лимита аватара до открытия RPC загрузки.
func TestUploadAgentAvatarRejectsOversizeBeforeRPC(t *testing.T) {
	command := &avatarCommandStub{stream: &avatarStreamStub{}}
	server := &Server{control: &controlplaneclient.Client{Command: command}}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/projects/prj_project01/agents/agt_employee01/avatar", bytes.NewReader([]byte("x")))
	request.Header.Set("Content-Type", "image/png")
	request.ContentLength = maximumAgentAvatarBytes + 1
	response := httptest.NewRecorder()

	server.UploadAgentAvatar(response, request, "prj_project01", "agt_employee01", generated.UploadAgentAvatarParams{})

	if response.Code != http.StatusRequestEntityTooLarge || command.calls != 0 {
		t.Fatalf("oversize avatar reached RPC: status=%d calls=%d", response.Code, command.calls)
	}
}
