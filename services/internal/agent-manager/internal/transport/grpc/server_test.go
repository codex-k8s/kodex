package grpc

import (
	"context"
	"testing"

	agentsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/agents/v1"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	grpcruntime "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRegisterAgentManagerService(t *testing.T) {
	t.Parallel()

	server := grpcruntime.NewServer()
	RegisterAgentManagerService(server, agentservice.New(agentservice.Config{}))
}

func TestNewServerRequiresService(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("NewServer(nil) did not panic")
		}
	}()
	_ = NewServer(nil)
}

func TestCreateFlowReturnsUnimplementedUntilBusinessSlice(t *testing.T) {
	t.Parallel()

	_, err := NewServer(agentservice.New(agentservice.Config{})).CreateFlow(context.Background(), &agentsv1.CreateFlowRequest{})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("CreateFlow() code = %s, want %s", status.Code(err), codes.Unimplemented)
	}
}

func TestServerKeepsDomainService(t *testing.T) {
	t.Parallel()

	agentService := agentservice.New(agentservice.Config{})
	if NewServer(agentService).service != agentService {
		t.Fatal("server did not keep composed domain service")
	}
}
