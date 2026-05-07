package grpc

import (
	"context"
	"testing"

	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	providerservice "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/service"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestServerEmbedsGeneratedUnimplementedContract(t *testing.T) {
	t.Parallel()

	server := NewServer(providerservice.New(&fakeRepository{}))
	_, err := server.GetWorkItemProjection(context.Background(), &providersv1.GetWorkItemProjectionRequest{})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("GetWorkItemProjection() code = %s, want unimplemented", status.Code(err))
	}
}

func TestNewServerPanicsWithoutService(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("NewServer() did not panic")
		}
	}()
	_ = NewServer(nil)
}

type fakeRepository struct{}

func (fakeRepository) Ping(context.Context) error {
	return nil
}
