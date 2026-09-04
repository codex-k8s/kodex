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

type avatarCommandStub struct {
	controlplanev1.PlatformCommandServiceClient
	stream *avatarStreamStub
	calls  int
}

func (client *avatarCommandStub) UploadAgentAvatar(context.Context, ...grpc.CallOption) (controlplanev1.PlatformCommandService_UploadAgentAvatarClient, error) {
	client.calls++
	return client.stream, nil
}

type avatarStreamStub struct {
	grpc.ClientStream
	messages []*controlplanev1.UploadAgentAvatarRequest
}

func (stream *avatarStreamStub) Send(message *controlplanev1.UploadAgentAvatarRequest) error {
	stream.messages = append(stream.messages, message)
	return nil
}

func (stream *avatarStreamStub) CloseAndRecv() (*controlplanev1.UploadAgentAvatarResponse, error) {
	return &controlplanev1.UploadAgentAvatarResponse{Agent: &controlplanev1.Agent{Ref: "agt_employee01", Version: 5}}, nil
}

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
	metadata := stream.messages[0].GetMetadata()
	commit := stream.messages[len(stream.messages)-1].GetCommit()
	if metadata.GetProjectRef() != "prj_project01" || metadata.GetAgentRef() != "agt_employee01" || metadata.GetSizeBytes() != int64(len(raw)) ||
		metadata.GetMutation().GetExpectedVersion() != 4 || commit.GetSizeBytes() != int64(len(raw)) || commit.GetSha256() == "" {
		t.Fatalf("atomic avatar envelope is invalid: metadata=%v commit=%v", metadata, commit)
	}
}

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
