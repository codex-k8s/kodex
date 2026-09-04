package httptransport

import (
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

func TestPublicRoleImageArtifactPreservesPromotionIdentity(t *testing.T) {
	t.Parallel()
	provenance := strings.Repeat("a", 64)
	artifact := publicRoleImageArtifact(&controlplanev1.ImageArtifact{
		Ref: "imgart_12345678", Version: 1, RecipeRef: "imgrec_12345678",
		RecipeGeneration: 2, ManifestDigest: "sha256:" + strings.Repeat("b", 64),
		ProvenanceSha256: provenance,
		AdmissionVerdict: controlplanev1.ImageAdmissionVerdict_IMAGE_ADMISSION_VERDICT_ACCEPTED,
	})

	if artifact.ProvenanceSha256 != provenance || artifact.AdmissionVerdict != "ACCEPTED" {
		t.Fatalf("promotion identity was not preserved: %#v", artifact)
	}
}
