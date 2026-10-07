package imageowner

import (
	"context"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	shared "github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type terminalRPCStub struct {
	cp.RoleImageServiceClient
	proof    *cp.GetImageAdmissionTerminalResponse
	readErr  error
	failCode codes.Code
	request  *cp.GetImageAdmissionTerminalRequest
	reads    int
	deadline bool
}

func (s *terminalRPCStub) FailImageAdmission(context.Context, *cp.FailImageAdmissionRequest, ...grpc.CallOption) (*cp.FailImageAdmissionResponse, error) {
	return nil, status.Error(s.failCode, "closed fixture denial")
}
func (s *terminalRPCStub) ExpireImageAdmissionClaim(context.Context, *cp.ExpireImageAdmissionClaimRequest, ...grpc.CallOption) (*cp.ExpireImageAdmissionClaimResponse, error) {
	return nil, status.Error(codes.PermissionDenied, "closed fixture denial")
}
func (s *terminalRPCStub) GetImageAdmissionTerminal(ctx context.Context, request *cp.GetImageAdmissionTerminalRequest, options ...grpc.CallOption) (*cp.GetImageAdmissionTerminalResponse, error) {
	s.reads++
	s.request = request
	_, s.deadline = ctx.Deadline()
	return s.proof, s.readErr
}

func TestSupersededAdmissionRecoveryRequiresExactOwnerTerminalProof(t *testing.T) {
	claim := Claim{ArtifactID: "imgart_12345678", Version: 2, Fence: 1, AuthorityGeneration: 4, ClaimToken: "private-fixture-token", RecipeID: "imgrec_12345678", RecipeGeneration: 1, BuildID: "imgbld_12345678", BuildAttempt: 1, ScopeKind: "PROJECT", OrganizationRef: "org_12345678", ProjectRef: "prj_12345678", ManifestDigest: "manifest-fixture", ImmutableBuildSHA256: "build-fixture", ProvenanceSHA256: "provenance-fixture", PolicyRevision: 1, PolicySHA256: "policy-fixture", SpecSHA256: "spec-fixture", AdmissionAttemptRef: "imgadm_12345678", AdmissionAttempt: 1, ExpiresAt: time.Now().Add(time.Hour)}
	proof := &cp.GetImageAdmissionTerminalResponse{TerminalState: cp.ImageAdmissionTerminalState_IMAGE_ADMISSION_TERMINAL_STATE_CANCELLED, ClaimedArtifact: &cp.ImageArtifact{Ref: claim.ArtifactID, Version: claim.Version, RecipeRef: claim.RecipeID, RecipeGeneration: claim.RecipeGeneration, BuildRef: claim.BuildID, BuildAttempt: claim.BuildAttempt, ScopeKind: cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT, OrganizationRef: claim.OrganizationRef, ProjectRef: claim.ProjectRef, ManifestDigest: claim.ManifestDigest, ImmutableBuildSha256: claim.ImmutableBuildSHA256, ProvenanceSha256: claim.ProvenanceSHA256, PolicyRevision: claim.PolicyRevision, PolicySha256: claim.PolicySHA256, SpecSha256: claim.SpecSHA256}, AdmissionAttemptRef: claim.AdmissionAttemptRef, AdmissionAttempt: 1, ClaimFence: claim.Fence, ClaimAuthorityGeneration: claim.AuthorityGeneration, TerminalArtifactVersion: 3, TerminalFence: 2, TerminalAttemptVersion: 3}
	for _, scenario := range []string{"terminal", "accepted", "rejected", "failed", "denial_only", "unknown_state", "wrong_build", "wrong_attempt", "wrong_project", "wrong_fence", "wrong_generation", "wrong_source", "wrong_risk", "live_version", "unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			p := proto.Clone(proof).(*cp.GetImageAdmissionTerminalResponse)
			stub := &terminalRPCStub{proof: p, failCode: codes.PermissionDenied}
			switch scenario {
			case "accepted":
				p.TerminalState = cp.ImageAdmissionTerminalState_IMAGE_ADMISSION_TERMINAL_STATE_ACCEPTED
			case "rejected":
				p.TerminalState = cp.ImageAdmissionTerminalState_IMAGE_ADMISSION_TERMINAL_STATE_REJECTED
			case "failed":
				p.TerminalState = cp.ImageAdmissionTerminalState_IMAGE_ADMISSION_TERMINAL_STATE_FAILED
			case "denial_only":
				stub.readErr = status.Error(codes.PermissionDenied, "closed fixture denial")
			case "unknown_state":
				p.TerminalState = 99
			case "wrong_build":
				p.ClaimedArtifact.BuildRef = "imgbld_foreign"
			case "wrong_attempt":
				p.AdmissionAttempt++
			case "wrong_project":
				p.ClaimedArtifact.ProjectRef = "prj_foreign"
			case "wrong_fence":
				p.ClaimFence++
			case "wrong_generation":
				p.ClaimAuthorityGeneration++
			case "live_version":
				p.TerminalArtifactVersion = claim.Version
			case "wrong_source":
				p.SourceAdmissionRevision++
			case "wrong_risk":
				p.RiskAcceptanceSha256 = "foreign"
			case "unavailable":
				stub.failCode = codes.Unavailable
			}
			client := &Client{shared: &shared.Client{RoleImages: stub}, rpcDeadline: time.Second}
			err := client.FailWithTerminalRecovery(t.Context(), "failure-key", "original-claim-key", claim, "ADMISSION_WORKER_FAILED")
			valid := scenario == "terminal" || scenario == "accepted" || scenario == "rejected" || scenario == "failed"
			if (err == nil) != valid {
				t.Fatalf("terminal proof boundary violated: %s err=%v", scenario, err)
			}
			if scenario == "unavailable" {
				if stub.reads != 0 {
					t.Fatal("unknown outcome became terminal")
				}
				return
			}
			if !stub.deadline || stub.reads != 1 || stub.request.ClaimIdempotencyKey != "original-claim-key" || stub.request.BuildRef != claim.BuildID || stub.request.ExpectedVersion != claim.Version || stub.request.ExpectedFence != claim.Fence || stub.request.ExpectedAuthorityGeneration != claim.AuthorityGeneration || stub.request.ExpectedAdmissionAttemptRef != claim.AdmissionAttemptRef || stub.request.SpecSha256 != claim.SpecSHA256 || stub.request.SourceAdmissionRevision != claim.SourceAdmissionRevision || stub.request.RiskAcceptanceSha256 != claim.RiskAcceptanceSHA256 {
				t.Fatal("terminal read lost original exact claim")
			}
		})
	}
}
