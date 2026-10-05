package httptransport

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"testing"
	"time"
)

func TestPublicRoleImageAdmissionFailureRequiresExactCurrentScope(t *testing.T) {
	now := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	scope := cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION
	recipe := &cp.RoleImageRecipe{Ref: "imgrec_12345678", Generation: 2, ScopeKind: scope, OrganizationRef: "org_12345678"}
	build := &cp.ImageBuild{Ref: "imgbld_12345678", RecipeRef: recipe.Ref, RecipeGeneration: 2, Attempt: 1, ScopeKind: scope, OrganizationRef: recipe.OrganizationRef, Stage: cp.ImageBuildStage_IMAGE_BUILD_STAGE_COMPLETED, CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now)}
	failure := &cp.RoleImageAdmissionFailure{ImageArtifactRef: "imgart_12345678", Version: 3, RecipeRef: recipe.Ref, RecipeGeneration: 2, BuildRef: build.Ref, BuildAttempt: 1, ScopeKind: scope, OrganizationRef: recipe.OrganizationRef, State: "FAILED", ErrorCode: "ADMISSION_WORKER_FAILED"}
	for _, scenario := range []string{"valid", "unknown_code", "fake_verdict", "foreign_org", "project_leak", "wrong_generation", "wrong_attempt", "no_build", "superseded_build"} {
		t.Run(scenario, func(t *testing.T) {
			f := proto.Clone(failure).(*cp.RoleImageAdmissionFailure)
			builds := []*cp.ImageBuild{proto.Clone(build).(*cp.ImageBuild)}
			switch scenario {
			case "unknown_code":
				f.ErrorCode = "PRIVATE_ERROR"
			case "fake_verdict":
				f.State = "REJECTED"
			case "foreign_org":
				f.OrganizationRef = "org_foreign1"
			case "project_leak":
				f.ProjectRef = "prj_foreign1"
			case "wrong_generation":
				f.RecipeGeneration++
			case "wrong_attempt":
				f.BuildAttempt++
			case "no_build":
				builds = nil
			case "superseded_build":
				latest := proto.Clone(build).(*cp.ImageBuild)
				latest.Ref = "imgbld_new12345"
				latest.CreatedAt = timestamppb.New(now.Add(time.Second))
				builds = append(builds, latest)
			}
			got, ok := publicRoleImageAdmissionFailure(f, recipe, builds)
			if ok != (scenario == "valid") || ((got != nil) != (scenario == "valid")) {
				t.Fatal("technical admission DTO boundary disagrees with exact current tuple")
			}
		})
	}
	if got, ok := publicRoleImageAdmissionFailure(nil, recipe, nil); !ok || got != nil {
		t.Fatal("missing failure is not optional")
	}
}
