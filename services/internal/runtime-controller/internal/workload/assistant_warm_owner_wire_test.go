package workload

import (
	"os"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"k8s.io/client-go/kubernetes/fake"
)

// TestAssistantWarmRuntimeOwnerWireBuild consumes an explicitly enabled
// disposable owner's protobuf-JSON snapshot without locally resealing its digest,
// then rejects changed image pins. Missing KODEX_TEST_WARM_WIRE_PATH skips this
// producer-dependent test and must not be reported as a successful wire check.
// TestAssistantWarmRuntimeOwnerWireBuild читает явно включённый protobuf-JSON
// снимок disposable-владельца без локальной перепривязки digest и отвергает
// изменённые image pins. Без KODEX_TEST_WARM_WIRE_PATH тест пропускается;
// это нельзя объявлять успешной проверкой producer wire.
func TestAssistantWarmRuntimeOwnerWireBuild(t *testing.T) {
	path := os.Getenv("KODEX_TEST_WARM_WIRE_PATH")
	if path == "" {
		t.Skip("disposable owner wire pipeline is not enabled")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("read synthetic warm wire failed")
	}
	revision := &cp.RuntimeRevisionSnapshot{}
	if err := protojson.Unmarshal(raw, revision); err != nil {
		t.Fatal(err)
	}
	manager := newTestManager(t, fake.NewSimpleClientset())
	input, _, err := manager.BuildWarmInput(revision)
	if err != nil {
		t.Fatalf("actual owner warm wire rejected: %v", err)
	}
	if input.RuntimeRevisionDigest != revision.GetRevisionDigest() || input.EnvironmentImage.ArtifactRef != revision.GetRoleImageArtifactRef() ||
		input.EnvironmentImage.RecipeRef != revision.GetRoleImageRecipeRef() || input.EnvironmentImage.RecipeGeneration != revision.GetRoleImageRecipeGeneration() {
		t.Fatal("warm build widened the owner's immutable image pins")
	}
	for name, mutate := range map[string]func(*cp.RuntimeRevisionSnapshot){
		"missing artifact":   func(r *cp.RuntimeRevisionSnapshot) { r.RoleImageArtifactRef = "" },
		"missing recipe":     func(r *cp.RuntimeRevisionSnapshot) { r.RoleImageRecipeRef = "" },
		"missing generation": func(r *cp.RuntimeRevisionSnapshot) { r.RoleImageRecipeGeneration = 0 },
		"foreign artifact":   func(r *cp.RuntimeRevisionSnapshot) { r.RoleImageArtifactRef += "foreign" },
		"foreign recipe":     func(r *cp.RuntimeRevisionSnapshot) { r.RoleImageRecipeRef += "foreign" },
		"foreign generation": func(r *cp.RuntimeRevisionSnapshot) { r.RoleImageRecipeGeneration++ },
	} {
		t.Run(name, func(t *testing.T) {
			changed := proto.Clone(revision).(*cp.RuntimeRevisionSnapshot)
			mutate(changed)
			if _, _, err := manager.BuildWarmInput(changed); err == nil {
				t.Fatal("changed owner warm image pin accepted")
			}
		})
	}
}
