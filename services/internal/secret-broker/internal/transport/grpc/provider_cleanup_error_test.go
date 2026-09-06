package grpc

import (
	"context"
	"errors"
	"testing"

	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCleanupSnapshotChangedErrorPreservesClosedNoEffectProof(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		err   error
		code  codes.Code
		proof bool
	}{
		{"CAS changed before fence", kubernetesstore.ErrProviderAuthorizationCleanupSnapshotChanged, codes.FailedPrecondition, true},
		{"generic conflict", kubernetesstore.ErrProviderCredentialCleanupConflict, codes.FailedPrecondition, false},
		{"invalid input", kubernetesstore.ErrProviderCredentialCleanupInvalid, codes.InvalidArgument, false},
		{"unknown outcome", context.DeadlineExceeded, codes.Unavailable, false},
		{"untrusted text", errors.New("CAS_SNAPSHOT_CHANGED"), codes.Unavailable, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result := status.Convert(providerCredentialCleanupError(test.err))
			if result.Code() != test.code {
				t.Fatalf("unexpected cleanup status: %s", result.Code())
			}
			if !test.proof {
				if len(result.Details()) != 0 {
					t.Fatal("generic failure acquired a no-effect proof")
				}
				return
			}
			if len(result.Details()) != 1 {
				t.Fatal("missing unique no-effect detail")
			}
			info, ok := result.Details()[0].(*errdetails.ErrorInfo)
			if !ok || info.Domain != "kodex.provider_credential_cleanup" || info.Reason != "CAS_SNAPSHOT_CHANGED" || len(info.Metadata) != 0 {
				t.Fatal("no-effect proof changed its public contract")
			}
		})
	}
}
