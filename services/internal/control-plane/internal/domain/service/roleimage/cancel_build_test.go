package roleimage

import (
	"testing"

	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestRoleImageManageIntentDigestBindsOwnerScope(t *testing.T) {
	for _, action := range []string{"CREATE", "UPDATE", "ARCHIVE", "RESTORE", "REQUEST_BUILD"} {
		input := roleimagerepo.ManageInput{ScopeKind: "PROJECT", Action: action, RecipeRef: "imgrec_example", ProjectRef: "prj_example",
			RoleDefinitionRef: "role_example", Name: "Example", Recipe: entity.RoleImageRecipeInput{EnvironmentKey: "standard"}}
		other := input
		other.ScopeKind = "ORGANIZATION"
		if roleImageManageIntentDigest(input) == roleImageManageIntentDigest(other) {
			t.Fatalf("%s idempotency digest does not bind owner scope", action)
		}
	}
	first := roleimagerepo.ManageInput{Action: "CANCEL_BUILD", RecipeRef: "imgrec_example", ProjectRef: "prj_example", BuildRef: "imgbld_first123"}
	second := first
	second.BuildRef = "imgbld_other123"
	if roleImageManageIntentDigest(first) == roleImageManageIntentDigest(second) {
		t.Fatal("cancellation digest does not bind the exact build")
	}
}
