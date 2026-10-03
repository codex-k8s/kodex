package grpc

import (
	"encoding/json"
	"os"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Публичная точка входа: scripts/tests/assistant-warm-wire-test.sh.
func TestAssistantWarmRuntimeOwnerSnapshotTransport(t *testing.T) {
	input, output := os.Getenv("KODEX_TEST_WARM_OWNER_SNAPSHOT_PATH"), os.Getenv("KODEX_TEST_WARM_WIRE_PATH")
	if input == "" || output == "" {
		t.Skip("disposable owner wire pipeline is not enabled")
	}
	raw, err := os.ReadFile(input)
	if err != nil {
		t.Fatal("read synthetic owner snapshot failed")
	}
	var values map[string]any
	var typed struct {
		EnvironmentImage          runtimecontract.RuntimeEnvironmentImage
		EnvironmentTools          []runtimecontract.RuntimeEnvironmentTool
		EnvironmentPolicy         runtimecontract.RuntimeEnvironmentPolicy
		EffectiveKubernetesAccess runtimecontract.RuntimeKubernetesAccess
		WorkspacePolicy           entity.RuntimeWorkspacePolicy
	}
	if json.Unmarshal(raw, &values) != nil || json.Unmarshal(raw, &typed) != nil {
		t.Fatal("decode synthetic owner snapshot failed")
	}
	values["environmentImage"], values["environmentTools"] = typed.EnvironmentImage, typed.EnvironmentTools
	values["environmentPolicy"], values["effectiveKubernetesAccess"] = typed.EnvironmentPolicy, typed.EffectiveKubernetesAccess
	values["workspacePolicy"] = typed.WorkspacePolicy
	revision := castRuntimeRevision(values)
	if revision == nil || revision.GetAssistantScope() != cp.AssistantScope_ASSISTANT_SCOPE_SYSTEM ||
		revision.GetRoleImageArtifactRef() != typed.EnvironmentImage.ArtifactRef || revision.GetRoleImageArtifactRef() == "" ||
		revision.GetRoleImageRecipeRef() != typed.EnvironmentImage.RecipeRef || revision.GetRoleImageRecipeRef() == "" ||
		revision.GetRoleImageRecipeGeneration() != typed.EnvironmentImage.RecipeGeneration || revision.GetRoleImageRecipeGeneration() < 1 ||
		revision.GetRevisionDigest() != mapString(values, "revisionDigest") {
		t.Fatal("owner warm snapshot pins were lost by the production caster")
	}
	wire, err := proto.Marshal(revision)
	if err != nil {
		t.Fatal(err)
	}
	received := &cp.RuntimeRevisionSnapshot{}
	if proto.Unmarshal(wire, received) != nil || !proto.Equal(revision, received) {
		t.Fatal("warm revision changed during protobuf transmission")
	}
	raw, err = protojson.Marshal(received)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("create synthetic warm wire output failed")
	}
	_, writeErr := file.Write(raw)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatal("write synthetic warm wire output failed")
	}
}
