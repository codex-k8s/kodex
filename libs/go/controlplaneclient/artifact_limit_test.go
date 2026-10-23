package controlplaneclient

import (
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/proto"
)

// TestProtectedResponseFitsMaximumSkillFile checks the gRPC limit against a large artifact.
// TestProtectedResponseFitsMaximumSkillFile сверяет лимит gRPC с крупным артефактом.
func TestProtectedResponseFitsMaximumSkillFile(t *testing.T) {
	response := &cp.ReadExecutionArtifactResponse{Artifact: &cp.Artifact{Ref: "art_fixture", FileName: "support.txt", SizeBytes: 32 << 20}, Content: make([]byte, 32<<20)}
	if proto.Size(response) > maximumProtectedResponseBytes {
		t.Fatal("maximum Skill file exceeds protected client response limit")
	}
}
