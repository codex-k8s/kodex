package grpc

import (
	"context"
	"testing"

	packagesv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/packages/v1"
	packageservice "github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/service"
	grpcruntime "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRegisterPackageHubService(t *testing.T) {
	t.Parallel()

	server := grpcruntime.NewServer()
	RegisterPackageHubService(server, packageservice.New())
}

func TestNewServerCreatesDefaultService(t *testing.T) {
	t.Parallel()

	if !NewServer(nil).ready() {
		t.Fatal("NewServer(nil) is not ready")
	}
}

func TestBacklogMethodReturnsUnimplemented(t *testing.T) {
	t.Parallel()

	response, err := NewServer(packageservice.New()).GetPackage(context.Background(), &packagesv1.GetPackageRequest{})
	if response != nil {
		t.Fatalf("GetPackage() response = %+v, want nil", response)
	}
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("GetPackage() code = %s, want %s", status.Code(err), codes.Unimplemented)
	}
}
