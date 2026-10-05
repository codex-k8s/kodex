package imageowner

import (
	"context"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	sharedclient "github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"testing"
	"time"
)

type failureRPCStub struct {
	cp.RoleImageServiceClient
	code                   codes.Code
	failCalls, expireCalls int
	expiry                 *cp.ExpireImageAdmissionClaimRequest
	failure                *cp.FailImageAdmissionRequest
}

func (s *failureRPCStub) FailImageAdmission(_ context.Context, in *cp.FailImageAdmissionRequest, _ ...grpc.CallOption) (*cp.FailImageAdmissionResponse, error) {
	s.failCalls++
	s.failure = in
	return nil, status.Error(s.code, "closed fixture failure")
}
func (s *failureRPCStub) ExpireImageAdmissionClaim(_ context.Context, in *cp.ExpireImageAdmissionClaimRequest, _ ...grpc.CallOption) (*cp.ExpireImageAdmissionClaimResponse, error) {
	s.expireCalls++
	s.expiry = in
	return &cp.ExpireImageAdmissionClaimResponse{AdmissionFailure: &cp.RoleImageAdmissionFailure{ImageArtifactRef: in.ImageArtifactRef, Version: in.ExpectedVersion + 1, RecipeRef: "imgrec_12345678", RecipeGeneration: in.RecipeGeneration, BuildRef: in.BuildRef, BuildAttempt: in.ExpectedBuildAttempt, ScopeKind: cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION, OrganizationRef: "org_12345678", State: "FAILED", ErrorCode: "ADMISSION_LEASE_EXPIRED"}}, nil
}
func TestAdmissionFailFallbackUsesDedicatedFreshExpiryAndExactTuple(t *testing.T) {
	claim := Claim{ArtifactID: "imgart_12345678", Version: 2, Fence: 3, AuthorityGeneration: 4, ClaimToken: "private-fixture-token", RecipeID: "imgrec_12345678", RecipeGeneration: 5, BuildID: "imgbld_12345678", BuildAttempt: 1, ScopeKind: "ORGANIZATION", OrganizationRef: "org_12345678", ManifestDigest: "manifest-fixture", ImmutableBuildSHA256: "build-fixture", ProvenanceSHA256: "provenance-fixture", PolicyRevision: 1, PolicySHA256: "policy-fixture", SpecSHA256: "spec-fixture"}
	claim.AdmissionAttemptRef, claim.AdmissionAttempt, claim.ExpiresAt = "imgadm_12345678", 2, time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC)
	for _, code := range []codes.Code{codes.PermissionDenied, codes.Unavailable, codes.Aborted, codes.InvalidArgument} {
		t.Run(code.String(), func(t *testing.T) {
			stub := &failureRPCStub{code: code}
			client := &Client{shared: &sharedclient.Client{RoleImages: stub}, rpcDeadline: time.Second}
			err := client.Fail(t.Context(), "failure-key", claim, "ADMISSION_WORKER_FAILED")
			if code == codes.PermissionDenied {
				if err != nil || stub.expireCalls != 1 || stub.expiry.ExpectedAuthorityGeneration != claim.AuthorityGeneration || stub.expiry.ExpectedFence != claim.Fence || stub.expiry.ExpectedVersion != claim.Version || stub.expiry.IdempotencyKey != "failure-key-expiry" || stub.expiry.ManifestDigest != claim.ManifestDigest || stub.expiry.BuildRef != claim.BuildID || stub.expiry.SpecSha256 != claim.SpecSHA256 {
					t.Fatal("dedicated expiry lost exact tuple or receipt")
				}
			} else if err == nil || stub.expireCalls != 0 {
				t.Fatal("unknown callback outcome incorrectly became owner expiry")
			}
			if stub.failCalls != 1 {
				t.Fatal("callback retry was unbounded")
			}
			if stub.failure.ExpectedAdmissionAttemptRef != claim.AdmissionAttemptRef || stub.failure.ExpectedAdmissionAttempt != claim.AdmissionAttempt ||
				stub.expiry != nil && (stub.expiry.ExpectedAdmissionAttemptRef != claim.AdmissionAttemptRef || stub.expiry.ExpectedAdmissionAttempt != claim.AdmissionAttempt) {
				t.Fatal("terminal callback lost exact admission attempt")
			}
		})
	}
}
