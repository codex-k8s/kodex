package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

func testModelCatalogVersion(t *testing.T, ctx context.Context, repository *Repository) {
	seedObservedCatalogFixture(t, ctx, repository)
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.query.models.list"}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.ListModelCatalog(ctx, owner, "openai-codex", "", query.Filter{Page: query.Page{Size: 2}})
	if err != nil || first.Total != 3 || first.NextPageToken == "" || len(first.NextPageToken) > 512 || len(first.Digest) != 64 || first.Revision != "mcat_"+first.Digest {
		t.Fatalf("first catalog: %v", err)
	}
	filter := query.Filter{Page: query.Page{Size: 2, Token: first.NextPageToken}, ExpectedCatalogRevision: first.Revision, ExpectedCatalogDigest: first.Digest}
	second, err := service.ListModelCatalog(ctx, owner, "openai-codex", "", filter)
	if err != nil || second.Digest != first.Digest || second.Revision != first.Revision || second.Models[0].ID == first.Models[0].ID {
		t.Fatalf("version-bound page: %v", err)
	}
	empty, err := service.ListModelCatalog(ctx, owner, "openai-codex", "", query.Filter{Query: "no-such-model"})
	if err != nil || empty.Total != 0 || empty.Digest != first.Digest {
		t.Fatalf("empty filtered page: %v", err)
	}
	filter.ExpectedCatalogDigest = "wrong"
	if _, err := service.ListModelCatalog(ctx, owner, "openai-codex", "", filter); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("mismatched pin: %v", err)
	}
	if _, err := repository.pool.Exec(ctx, `UPDATE control_plane.provider_definitions SET version=version+1 WHERE stable_key='openai-codex'`); err != nil {
		t.Fatal(err)
	}
	filter.ExpectedCatalogDigest = first.Digest
	if _, err := service.ListModelCatalog(ctx, owner, "openai-codex", "", filter); err != nil {
		t.Fatalf("metadata-only update changed capabilities pin: %v", err)
	}
	if _, err := repository.pool.Exec(ctx, queryCatalogFixtureAdvanceAccount, owner.AuthorityTenant, first.Models[0].EligibleProviderAccountRefs[0]); err != nil {
		t.Fatal(err)
	}
	seedObservedCatalogFixture(t, ctx, repository, func(observation *platformrepo.ProviderModelCatalogObservation) {
		observation.Models[0].DefaultReasoningEffort = "low"
	})
	if _, err := service.ListModelCatalog(ctx, owner, "openai-codex", "", filter); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("stale source pin: %v", err)
	}
	filter.ExpectedCatalogRevision, filter.ExpectedCatalogDigest = "", ""
	if _, err := service.ListModelCatalog(ctx, owner, "openai-codex", "", filter); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("stale cursor: %v", err)
	}
	latest, err := service.ListModelCatalog(ctx, owner, "openai-codex", "", query.Filter{})
	if err != nil || latest.Digest == first.Digest {
		t.Fatalf("new source revision: %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := service.ListModelCatalog(canceled, owner, "openai-codex", "", query.Filter{}); err == nil {
		t.Fatal("cancellation accepted")
	}
	if _, err := repository.pool.Exec(ctx, queryCatalogFixtureAdvanceAccount, owner.AuthorityTenant, first.Models[0].EligibleProviderAccountRefs[0]); err != nil {
		t.Fatal(err)
	}
	seedObservedCatalogFixture(t, ctx, repository)
}

func TestModelCatalogCursorBindsAuthority(t *testing.T) {
	first := scope{organizationID: "tenant-a", actorID: "actor-a"}
	token := encodeModelCapabilityCursor("provider", "model", "provider", "", modelCatalogActorFilter(first, "query"), "mcat_revision", "digest")
	for _, current := range []scope{{organizationID: "tenant-b", actorID: "actor-a"}, {organizationID: "tenant-a", actorID: "actor-b"}, {organizationID: "tenant-a", actorID: "actor-a", authorityProjectID: "project-b"}} {
		if _, err := decodeModelCapabilityCursor(token, "provider", "", modelCatalogActorFilter(current, "query")); !errors.Is(err, errs.ErrInvalid) {
			t.Fatal("cursor crossed authority")
		}
	}
}
