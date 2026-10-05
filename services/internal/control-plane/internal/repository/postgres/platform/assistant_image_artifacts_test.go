package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	roleimageservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// Одинаковый discovery path проверяется для собственного PROJECT и SYSTEM.
func testAssistantCatalogCandidatePromotion(t *testing.T, ctx context.Context, r *Repository, service *platformservice.Service,
	owner, reader value.Principal, lease map[string]any, catalog *roleimageservice.Catalog, detail roleimagerepo.Detail,
	prefix string, readCatalog func(string) entity.AssistantConfigurationCatalogResponse) {
	t.Helper()
	t.Run("candidate evidence appears only after exact owner promotion", func(t *testing.T) {
		resolved, err := r.ResolvePrincipal(ctx, owner)
		if err != nil {
			t.Fatal(err)
		}
		artifact := seedAdmittedPromotionArtifact(t, ctx, r, resolved, detail.Recipe, detail.Builds[0])
		for _, entry := range readCatalog("IMAGE_ARTIFACTS").Entries {
			if entry.Ref == artifact.Ref {
				t.Fatal("unpromoted admission candidate disclosed")
			}
		}
		images, err := roleimageservice.New(r, catalog)
		if err != nil {
			t.Fatal(err)
		}
		promoteOwner := owner
		requestPromotion := images.Promote
		promoteOwner.Permission = "platform.command.role-images.promote"
		if detail.Recipe.ScopeKind == "ORGANIZATION" {
			requestPromotion = images.RequestOrganizationPromotion
			promoteOwner.Permission = "platform.command.organization.role-images.promote"
		}
		expected := int64(detail.Recipe.Version)
		if _, err := requestPromotion(ctx, roleimagerepo.PromotionRequestInput{Principal: promoteOwner,
			Mutation:  value.Mutation{IdempotencyKey: prefix + "-inventory-request", ExpectedVersion: &expected},
			RecipeRef: detail.Recipe.Ref, ArtifactRef: artifact.Ref, ExpectedProvenanceSHA256: artifact.ProvenanceSHA256}); err != nil {
			t.Fatal(err)
		}
		promoter := resolved
		promoter.CallerWorkload, promoter.Permission = "image-promotion", "platform.role-images.promotion.claim"
		claim, err := r.ClaimPromotion(ctx, promoter, prefix+"-inventory-claim")
		if err != nil || claim.Artifact.Ref != artifact.Ref {
			t.Fatal("synthetic exact candidate claim failed")
		}
		authorized, err := r.AuthorizePromotion(ctx, roleimagerepo.PromotionAuthorizeInput{Principal: promoter,
			IdempotencyKey: prefix + "-inventory-authorize", ArtifactRef: artifact.Ref, PromotionClaim: claim.PromotionClaim,
			ManifestDigest: artifact.ManifestDigest, ExpectedVersion: claim.Artifact.Version})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.CompletePromotion(ctx, roleimagerepo.PromotionCompleteInput{Principal: promoter,
			IdempotencyKey: prefix + "-inventory-complete", ArtifactRef: artifact.Ref, AuthorizationToken: authorized.AuthorizationToken,
			ManifestDigest: artifact.ManifestDigest, ExpectedVersion: authorized.Artifact.Version,
			PromotedReference: r.roleImages.PromotedRepository + "@" + artifact.ManifestDigest, PromotionReadbackSHA256: strings.Repeat("9", 64)}); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, entry := range readCatalog("IMAGE_ARTIFACTS").Entries {
			if entry.Ref != artifact.Ref {
				continue
			}
			found = true
			if entry.ToolInventory == nil || entry.ToolInventorySHA256 != artifact.ToolInventorySHA256 ||
				entry.ToolInventory.ProvenanceSHA256 != artifact.ProvenanceSHA256 ||
				len(entry.ToolInventory.Platforms[0].Manifest.Tools) != len(runtimecontract.ImageToolProbes()) {
				t.Fatal("promoted candidate lost complete exact inventory")
			}
		}
		if !found {
			t.Fatal("promoted candidate missing from own catalog")
		}
		foreign := reader
		foreign.AuthorityTenant = "30000000-0000-4000-8000-000000000099"
		result, err := service.ListAssistantConfigurationCatalog(ctx, foreign, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), lease["generation"].(int64),
			entity.AssistantConfigurationCatalogRequest{Kind: "IMAGE_ARTIFACTS", AssistantRef: stringMap(lease, "agentRef")})
		if err == nil || len(result.Entries) != 0 {
			t.Fatal("foreign tenant read candidate inventory")
		}
	})
}

func TestAssistantCatalogArtifactRequiresExactEligibleOwnerAndPins(t *testing.T) {
	digest := strings.Repeat("a", 64)
	entry := entity.AssistantConfigurationCatalogEntry{Ref: "imgart_owned123", Version: 3, RecipeGeneration: 8,
		OrganizationRef: "org_owned123", ScopeKind: "ORGANIZATION", ManifestDigest: "sha256:" + digest, Reference: "pull.fixture.invalid/roles@sha256:" + digest}
	artifact := entity.ImageArtifact{Ref: entry.Ref, Version: 3, RecipeGeneration: 8, OrganizationRef: entry.OrganizationRef, ScopeKind: entry.ScopeKind,
		ManifestDigest: entry.ManifestDigest, PromotedReference: entry.Reference, AdmissionVerdict: "ACCEPTED", PromotionState: "PROMOTED", PromotionReadbackSHA256: digest}
	projected := entry
	if err := projectAssistantCatalogArtifact(&projected, artifact); err != nil || projected.AdmissionVerdict != "ACCEPTED" || projected.PromotionState != "PROMOTED" ||
		projected.ToolInventory != nil || projected.ToolInventorySHA256 != "" {
		t.Fatal("eligible historical artifact invented missing inventory")
	}
	for _, testcase := range []struct {
		name   string
		mutate func(*entity.ImageArtifact)
	}{
		{"foreign owner", func(a *entity.ImageArtifact) { a.OrganizationRef = "org_foreign123" }},
		{"foreign scope", func(a *entity.ImageArtifact) { a.ScopeKind = "PROJECT"; a.ProjectRef = "prj_foreign123" }},
		{"foreign ref", func(a *entity.ImageArtifact) { a.Ref = "imgart_foreign123" }},
		{"stale version", func(a *entity.ImageArtifact) { a.Version++ }},
		{"stale generation", func(a *entity.ImageArtifact) { a.RecipeGeneration++ }},
		{"foreign manifest", func(a *entity.ImageArtifact) { a.ManifestDigest = "sha256:" + strings.Repeat("b", 64) }},
		{"foreign reference", func(a *entity.ImageArtifact) { a.PromotedReference += "foreign" }},
		{"rejected", func(a *entity.ImageArtifact) { a.AdmissionVerdict = "REJECTED" }},
		{"unpromoted", func(a *entity.ImageArtifact) { a.PromotionState = "PENDING" }},
		{"missing receipt", func(a *entity.ImageArtifact) { a.PromotionReadbackSHA256 = "" }},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			value, output := artifact, entry
			testcase.mutate(&value)
			if err := projectAssistantCatalogArtifact(&output, value); !errors.Is(err, errs.ErrUnavailable) ||
				output.AdmissionVerdict != "" || output.PromotionState != "" || output.ToolInventory != nil {
				t.Fatal("detached candidate or partial evidence disclosed")
			}
		})
	}
}

func TestAssistantCatalogInventoryRejectsCorruptionAndForeignProvenance(t *testing.T) {
	digest := strings.Repeat("a", 64)
	artifact := entity.ImageArtifact{SpecSHA256: digest, ImmutableBuildSHA256: digest, RoleRuntimeContractSHA256: digest,
		ManifestDigest: "sha256:" + digest, ProvenanceSHA256: digest, Platforms: []entity.RoleImagePlatform{{OS: "linux", Architecture: "amd64"}}}
	raw, hash := imageInventoryFixture(artifact)
	artifact.ToolInventorySHA256 = hash
	for _, testcase := range []struct {
		name   string
		raw    string
		mutate func(*entity.ImageArtifact)
	}{
		{"corrupt inventory", "{", func(*entity.ImageArtifact) {}},
		{"foreign hash", raw, func(a *entity.ImageArtifact) { a.ToolInventorySHA256 = strings.Repeat("b", 64) }},
		{"foreign provenance", raw, func(a *entity.ImageArtifact) { a.ProvenanceSHA256 = strings.Repeat("b", 64) }},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			candidate := artifact
			testcase.mutate(&candidate)
			if hydrateArtifactToolInventory(&candidate, testcase.raw) == nil || candidate.ToolInventory != nil {
				t.Fatal("corrupt catalog inventory or detached provenance disclosed")
			}
		})
	}
}
