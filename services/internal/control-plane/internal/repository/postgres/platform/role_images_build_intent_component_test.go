package platform

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	roleimageservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestRoleImageBuildFreshCorrelationComponent runs complete/fail build scenarios in an isolated
// database with a forty-five-second budget. Fresh CorrelationRef must replay progress, renew and
// terminal results exactly, while changed actor/workload/credential revision/tenant is denied;
// final owner readback requires one persisted build at the original terminal version.
// TestRoleImageBuildFreshCorrelationComponent выполняет complete/fail build сценарии в
// изолированной БД с бюджетом сорок пять секунд. Свежий CorrelationRef должен точно повторять
// progress, renew и terminal results, а смена actor/workload/credential revision/tenant
// отклоняется; финальный owner readback требует единственную сохранённую сборку с исходной
// terminal version.
func TestRoleImageBuildFreshCorrelationComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigureRoleImages(RoleImageConfig{
		PolicyRevision: 1, RoleRuntimeContractRevision: 1, PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64),
		BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3,
		StagingRepository: "registry.invalid/kodex/staging", PromotedRepository: "registry.invalid/kodex/roles",
		DefaultImageReference: "registry.invalid/kodex/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32)),
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.organization.role-images.recipes.manage",
	}, "control-api-gateway")
	resolved, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	catalog, specification := promotionComponentCatalog(t)
	service, err := roleimageservice.New(repository, catalog)
	if err != nil {
		t.Fatal(err)
	}
	for _, terminal := range []string{"complete", "fail"} {
		t.Run(terminal, func(t *testing.T) {
			created, err := repository.ManageOrganization(ctx, roleimagerepo.ManageInput{
				Principal: resolved, Action: "CREATE", Name: "Fresh correlation " + terminal, Recipe: specification,
				Mutation: roleImageTestMutation("fresh-correlation-create-"+terminal, "CREATE", nil),
			})
			if err != nil {
				t.Fatal(err)
			}
			worker := owner
			worker.CallerWorkload, worker.Permission = "role-image-builder", "platform.role-images.builds.claim"
			claim, err := service.ClaimBuild(ctx, worker, "fresh-correlation-claim-"+terminal)
			if err != nil || claim.Build.Ref != created.Build.Ref {
				t.Fatalf("claim fixture: %v", err)
			}
			worker.Permission = "platform.role-images.builds.progress"
			lease := roleimagerepo.BuildLeaseInput{Principal: worker, IdempotencyKey: "fresh-correlation-report-" + terminal,
				BuildRef: claim.Build.Ref, LeaseToken: claim.LeaseToken, ExpectedVersion: claim.Build.Version,
				ExpectedAttempt: claim.Build.Attempt, ExpectedFence: claim.Fence}
			progress := roleimagerepo.BuildProgressInput{BuildLeaseInput: lease, Stage: "CONTEXT_VALIDATION", ProgressPercent: 15}
			first, err := service.ReportBuildProgress(ctx, progress)
			if err != nil {
				t.Fatal(err)
			}
			progress.Principal.CorrelationRef = "fresh-report-transport-correlation"
			for _, negative := range []struct {
				name   string
				change func(*value.Principal)
			}{
				{"actor", func(p *value.Principal) { p.ActorID = "20000000-0000-4000-8000-000000009999" }},
				{"workload", func(p *value.Principal) { p.CallerWorkload = "foreign-worker" }},
				{"revision", func(p *value.Principal) { p.CredentialRevision++ }},
				{"organization", func(p *value.Principal) { p.AuthorityTenant = "20000000-0000-4000-8000-000000009998" }},
			} {
				changed := progress
				negative.change(&changed.Principal)
				if _, err := service.ReportBuildProgress(ctx, changed); err == nil {
					t.Fatalf("%s replay accepted", negative.name)
				}
			}
			replay, err := service.ReportBuildProgress(ctx, progress)
			if err != nil || !reflect.DeepEqual(first, replay) {
				t.Fatalf("fresh report replay: %v", err)
			}
			lease.ExpectedVersion, lease.IdempotencyKey = first.Version, "fresh-correlation-renew-"+terminal
			lease.Principal.Permission = "platform.role-images.builds.renew"
			renewed, err := service.RenewBuild(ctx, lease)
			if err != nil {
				t.Fatal(err)
			}
			lease.Principal.CorrelationRef = "fresh-renew-transport-correlation"
			renewReplay, err := service.RenewBuild(ctx, lease)
			if err != nil || !reflect.DeepEqual(renewed, renewReplay) {
				t.Fatalf("fresh renew replay: %v", err)
			}
			lease.ExpectedVersion, lease.LeaseToken = renewed.Build.Version, renewed.LeaseToken
			lease.IdempotencyKey, lease.Principal.Permission = "fresh-correlation-terminal-"+terminal, "platform.role-images.builds."+terminal
			var result entity.ImageBuild
			if terminal == "complete" {
				input := roleimagerepo.BuildCompletionInput{BuildLeaseInput: lease,
					ManifestDigest: "sha256:" + strings.Repeat("e", 64), StagingReference: "registry.invalid/kodex/staging@sha256:" + strings.Repeat("e", 64),
					ProvenanceSHA256: strings.Repeat("f", 64), ImmutableBuildSHA256: claim.Build.ImmutableBuildSHA256}
				var artifact entity.ImageArtifact
				result, artifact, err = service.CompleteBuild(ctx, input)
				if err != nil {
					t.Fatal(err)
				}
				input.Principal.CorrelationRef = "fresh-complete-transport-correlation"
				second, secondArtifact, err := service.CompleteBuild(ctx, input)
				if err != nil || !reflect.DeepEqual(result, second) || !reflect.DeepEqual(artifact, secondArtifact) {
					t.Fatalf("fresh complete replay: %v", err)
				}
			} else {
				input := roleimagerepo.BuildFailureInput{BuildLeaseInput: lease, ErrorCode: "SOLVE_FAILED", DiagnosticCode: "BUILD_GRAPH_REJECTED", DiagnosticSummary: "Build solve failed"}
				result, err = service.FailBuild(ctx, input)
				if err != nil {
					t.Fatal(err)
				}
				input.Principal.CorrelationRef = "fresh-fail-transport-correlation"
				second, err := service.FailBuild(ctx, input)
				if err != nil || !reflect.DeepEqual(result, second) {
					t.Fatalf("fresh fail replay: %v", err)
				}
			}
			detail, err := repository.GetOrganization(ctx, resolved, created.Recipe.Ref)
			if err != nil || len(detail.Builds) != 1 || detail.Builds[0].Version != result.Version {
				t.Fatalf("replay changed persisted build: %v", err)
			}
		})
	}
}
