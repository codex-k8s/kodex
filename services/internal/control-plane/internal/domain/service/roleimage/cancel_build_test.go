package roleimage

import (
	"testing"

	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestRoleImageManageIntentDigestPreservesExistingReceipts(t *testing.T) {
	for _, action := range []string{"CREATE", "UPDATE", "ARCHIVE", "RESTORE", "REQUEST_BUILD"} {
		input := roleimagerepo.ManageInput{Action: action, RecipeRef: "imgrec_example", ProjectRef: "prj_example",
			RoleDefinitionRef: "role_example", Name: "Example", Recipe: entity.RoleImageRecipeInput{EnvironmentKey: "standard"}}
		previous := digest(struct {
			Action, RecipeRef, ProjectRef, RoleDefinitionRef, Name string
			Recipe                                                 entity.RoleImageRecipeInput
		}{input.Action, input.RecipeRef, input.ProjectRef, input.RoleDefinitionRef, input.Name, input.Recipe})
		if got := roleImageManageIntentDigest(input); got != previous {
			t.Fatalf("%s changed an existing idempotency digest", action)
		}
	}
	first := roleimagerepo.ManageInput{Action: "CANCEL_BUILD", RecipeRef: "imgrec_example", ProjectRef: "prj_example", BuildRef: "imgbld_first123"}
	second := first
	second.BuildRef = "imgbld_other123"
	if roleImageManageIntentDigest(first) == roleImageManageIntentDigest(second) {
		t.Fatal("cancellation digest does not bind the exact build")
	}
}
