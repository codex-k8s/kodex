package app

import (
	"testing"

	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestArchiveReadinessRequiresSuccessfulOwnerRPC(t *testing.T) {
	readiness := serviceruntime.NewReadiness()
	metrics := sharedobservability.NewMetrics("session_archive", "synthetic", map[string]string{})
	for _, err := range []error{status.Error(codes.DeadlineExceeded, "synthetic"), status.Error(codes.PermissionDenied, "synthetic"), nil, status.Error(codes.Unavailable, "synthetic"), nil} {
		setArchiveClaimReadiness(readiness, metrics, err)
		ready, reason := readiness.Ready()
		if ready != (err == nil) || err != nil && reason != "control_plane_unavailable" {
			t.Fatalf("unexpected archive readiness: ready=%v reason=%s", ready, reason)
		}
	}
}
