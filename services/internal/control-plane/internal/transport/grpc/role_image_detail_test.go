package grpc

import (
	"fmt"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	repo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/proto"
)

// TestRoleImageRecipeDetailProjectionPreservesShape checks all optional-field
// combinations with nil, empty and ordered builds against the original inline
// projection. It preserves protobuf presence/order, not domain eligibility or
// a real principal-bound read; no service or provider is contacted.
// TestRoleImageRecipeDetailProjectionPreservesShape проверяет все сочетания
// необязательных полей с nil, пустыми и упорядоченными сборками против прежнего
// встроенного преобразования. Сохраняет presence/order protobuf, не доменный
// допуск или реальное чтение с principal; сервисы и провайдеры не вызываются.
func TestRoleImageRecipeDetailProjectionPreservesShape(t *testing.T) {
	t.Parallel()
	for index, builds := range [][]entity.ImageBuild{
		nil, {}, {{Ref: "build_first", Attempt: 2}, {Ref: "build_second", Attempt: 1}},
	} {
		for mask := 0; mask < 8; mask++ {
			t.Run(fmt.Sprintf("builds_%d/optionals_%d", index, mask), func(t *testing.T) {
				detail := repo.Detail{Recipe: entity.RoleImageRecipe{Ref: "recipe_fixture", ScopeKind: "ORGANIZATION"}, Builds: builds}
				if mask&1 != 0 {
					detail.ActiveArtifact = &entity.ImageArtifact{Ref: "active_fixture", ManifestDigest: "active_digest"}
				}
				if mask&2 != 0 {
					detail.PromotionCandidate = &entity.ImageArtifact{Ref: "candidate_fixture", ManifestDigest: "candidate_digest"}
				}
				if mask&4 != 0 {
					detail.AdmissionFailure = &entity.RoleImageAdmissionFailure{ImageArtifactRef: "failure_fixture", ErrorCode: "failure_code"}
				}
				got := castRoleImageRecipeDetail(detail)
				actual := &cp.GetRoleImageRecipeResponse{
					Recipe: got.Recipe, AdmissionFailure: got.AdmissionFailure,
					Builds: got.Builds, ActiveArtifact: got.ActiveArtifact, PromotionCandidate: got.PromotionCandidate,
				}
				expected := &cp.GetRoleImageRecipeResponse{Recipe: castRoleImageRecipe(detail.Recipe), AdmissionFailure: castRoleImageAdmissionFailure(detail.AdmissionFailure)}
				for _, item := range detail.Builds {
					expected.Builds = append(expected.Builds, castImageBuild(item))
				}
				if detail.ActiveArtifact != nil {
					expected.ActiveArtifact = castImageArtifact(*detail.ActiveArtifact)
				}
				if detail.PromotionCandidate != nil {
					expected.PromotionCandidate = castImageArtifact(*detail.PromotionCandidate)
				}
				if !proto.Equal(actual, expected) || (got.Builds == nil) != (expected.Builds == nil) {
					t.Fatal("role image detail projection changed presence or order")
				}
				if (got.ActiveArtifact == nil) != (mask&1 == 0) || (got.PromotionCandidate == nil) != (mask&2 == 0) || (got.AdmissionFailure == nil) != (mask&4 == 0) {
					t.Fatal("role image detail optional presence changed")
				}
			})
		}
	}
}
