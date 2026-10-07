package grpc

import (
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	repo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestImageAdmissionTerminalCasterHasClosedRoundTrip(t *testing.T) {
	for _, state := range []string{"ACCEPTED", "REJECTED", "FAILED", "CANCELLED", "CLAIMED", "PENDING", "", "UNKNOWN"} {
		t.Run(state, func(t *testing.T) {
			proof := repo.AdmissionTerminalProof{State: state, AttemptRef: "imgadm_fixture", Attempt: 2, ClaimFence: 3, ClaimAuthorityGeneration: 4, TerminalArtifactVersion: 5, TerminalFence: 6, TerminalAttemptVersion: 7, RiskAcceptanceSHA256: "risk", SourceAdmissionRevision: 8, SourceAdmissionReceiptSHA256: "receipt", SourceEvidenceManifestDigest: "evidence"}
			response, err := castImageAdmissionTerminal(proof)
			terminal := state == "ACCEPTED" || state == "REJECTED" || state == "FAILED" || state == "CANCELLED"
			if !terminal {
				if status.Code(err) != codes.Internal || response != nil {
					t.Fatal("unknown or live state became terminal")
				}
				return
			}
			if err != nil || response.GetTerminalState() == cp.ImageAdmissionTerminalState_IMAGE_ADMISSION_TERMINAL_STATE_UNSPECIFIED {
				t.Fatal("closed terminal enum lost")
			}
			wire, err := proto.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			decoded := new(cp.GetImageAdmissionTerminalResponse)
			if err := proto.Unmarshal(wire, decoded); err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(response, decoded) || decoded.GetAdmissionAttemptRef() != proof.AttemptRef || decoded.GetClaimFence() != proof.ClaimFence || decoded.GetClaimAuthorityGeneration() != proof.ClaimAuthorityGeneration || decoded.GetTerminalArtifactVersion() != proof.TerminalArtifactVersion || decoded.GetTerminalFence() != proof.TerminalFence || decoded.GetTerminalAttemptVersion() != proof.TerminalAttemptVersion || decoded.GetRiskAcceptanceSha256() != proof.RiskAcceptanceSHA256 || decoded.GetSourceAdmissionRevision() != proof.SourceAdmissionRevision || decoded.GetSourceAdmissionReceiptSha256() != proof.SourceAdmissionReceiptSHA256 || decoded.GetSourceEvidenceManifestDigest() != proof.SourceEvidenceManifestDigest {
				t.Fatal("pinned terminal proof lost in protobuf round trip")
			}
		})
	}
}
