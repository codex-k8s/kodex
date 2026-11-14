package platform

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// TestModelDiscoveryEligibilityComponent reads the discovery service against an isolated migrated PostgreSQL owner registry; current account disable/re-enable/revoke never erases its exact descriptor and foreign authority returns no static metadata. Credential pins and catalog observations are synthetic, with no external calls.
// TestModelDiscoveryEligibilityComponent читает discovery service через изолированный мигрированный PostgreSQL owner registry; disable/re-enable/revoke account не стирают exact descriptor, а чужие полномочия не получают статические metadata. Credential pins и catalog observations синтетические; внешних вызовов нет.
func TestModelDiscoveryEligibilityComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	pool := newIsolatedAssistantComponentPool(t, ctx)
	defer pool.Close()
	repository, err := New(pool, "opencode-synthetic", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	configureComponentArtifactScanner(t, repository)
	configureComponentProviderAndRoleImages(t, repository)
	if err = repository.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	ownerFor := func(operation string) value.Principal {
		return resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: operation}, "control-api-gateway")
	}
	owner := ownerFor("platform.query.model-discovery.list")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	read := func(expected string) {
		t.Helper()
		page, readErr := service.ListModelDiscovery(ctx, owner, query.Filter{Query: "gpt-5"}, query.ModelDiscoveryFilter{ProviderID: "opencode-synthetic"})
		if readErr != nil || len(page.Items) != 1 {
			t.Fatalf("discovery owner read: count=%d err=%v", len(page.Items), readErr)
		}
		row := page.Items[0]
		if !row.RouteConfigured || row.ModelID != "gpt-5" || row.Known != "CUSTOM" || row.Account != expected || row.Allowed != "UNKNOWN" || row.Tested != "NOT_RUN" {
			t.Fatalf("independent discovery evidence: route=%t account=%s expected=%s", row.RouteConfigured, row.Account, expected)
		}
	}
	read("UNKNOWN")
	seedObservedCatalogFixture(t, ctx, repository)
	read("AVAILABLE")
	accounts, _, _, err := service.ListProviderAccounts(ctx, ownerFor("platform.query.provider-accounts.list"), query.Filter{})
	if err != nil || len(accounts) != 1 {
		t.Fatalf("fixture account count=%d err=%v", len(accounts), err)
	}
	account := executeProviderEnabledTransition(t, ctx, service, ownerFor("platform.command.provider-accounts.enable"), accounts[0], false, "discovery-disable")
	read("UNAVAILABLE")
	account = executeProviderEnabledTransition(t, ctx, service, ownerFor("platform.command.provider-accounts.enable"), account, true, "discovery-enable")
	read("UNAVAILABLE")
	seedObservedCatalogFixture(t, ctx, repository)
	read("AVAILABLE")
	_, err = service.Execute(ctx, command.Command{Kind: command.RevokeProviderAccount, Principal: ownerFor("platform.command.provider-accounts.revoke"), Mutation: value.Mutation{IdempotencyKey: "discovery-revoke", ExpectedVersion: &account.Version}, Payload: command.ProviderAccountInput{AccountRef: account.Ref}})
	if err != nil {
		t.Fatal("fixture owner revoke failed", err)
	}
	read("UNAVAILABLE")
	for _, foreign := range []value.Principal{{ActorID: owner.ActorID, AuthorityTenant: "20000000-0000-4000-8000-000000000099"}, {ActorID: "20000000-0000-4000-8000-000000000098", AuthorityTenant: owner.AuthorityTenant}} {
		foreign.Permission, foreign.CallerWorkload, foreign.CredentialRevision, foreign.CorrelationRef = owner.Permission, owner.CallerWorkload, owner.CredentialRevision, owner.CorrelationRef
		page, readErr := service.ListModelDiscovery(ctx, foreign, query.Filter{}, query.ModelDiscoveryFilter{})
		if !errors.Is(readErr, errs.ErrForbidden) || len(page.Items) != 0 {
			t.Fatal("foreign authority received static discovery metadata")
		}
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if page, readErr := service.ListModelDiscovery(canceled, owner, query.Filter{}, query.ModelDiscoveryFilter{}); readErr == nil || len(page.Items) != 0 {
		t.Fatal("canceled owner read returned discovery metadata")
	}
}
