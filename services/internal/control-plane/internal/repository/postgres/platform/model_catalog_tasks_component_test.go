package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/providercredentialclient"
)

//go:embed testdata/sql/model_catalog_insert_expired_task.sql
var queryCatalogFixtureExpiredTask string

//go:embed testdata/sql/model_catalog_mutate_claim.sql
var queryCatalogFixtureMutateClaim string

//go:embed testdata/sql/model_catalog_task_state.sql
var queryCatalogFixtureTaskState string

//go:embed testdata/sql/model_catalog_credential_id.sql
var queryCatalogFixtureCredentialID string

//go:embed testdata/sql/model_catalog_advance_account.sql
var queryCatalogFixtureAdvanceAccount string

//go:embed testdata/sql/model_catalog_account_id.sql
var queryCatalogFixtureAccountID string

//go:embed testdata/sql/model_catalog_warm_account.sql
var queryCatalogFixtureWarmAccount string

func prepareObservedWarmFixture(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	seedObservedCatalogFixture(t, ctx, repository)
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.warm.reconcile",
	}, "runtime-controller")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := service.ReconcileWarmRuntime(ctx, worker, "catalog-observed-warm-fixture"); err != nil {
		t.Fatal(err)
	}
}

func testCatalogOwnerProbe(t *testing.T, ctx context.Context, repository *Repository) {
	encoder := &providercredentialclient.Client{}
	tasks, err := repository.ClaimProviderModelCatalogTasks(ctx, "catalog-fixture", 1, encoder)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("claim catalog: %d %v", len(tasks), err)
	}
	task := tasks[0]
	principal := platformrepo.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "control-plane", Operation: platformrepo.ProviderModelCatalogOperation, RequestDigestSHA256: task.RequestDigest}
	if _, err := repository.ResolveProviderModelCatalogProof(ctx, principal); err != nil {
		t.Fatalf("active owner proof: %v", err)
	}
	wrong := principal
	wrong.RequestDigestSHA256 = strings.Repeat("f", 64)
	if _, err := repository.ResolveProviderModelCatalogProof(ctx, wrong); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("foreign digest: %v", err)
	}
	observation := platformrepo.ProviderModelCatalogObservation{AccountRef: task.AccountRef, CredentialRef: task.CredentialRef, Source: "REMOTE_CODEX", Failure: "NONE", ObservedAt: time.Now(), Models: []platformrepo.ProviderModelCatalogRecord{{ID: "gpt-5", DefaultReasoningEffort: "high", ReasoningEfforts: []string{"low", "medium", "high"}}, {ID: "future-model", DefaultReasoningEffort: "adaptive", ReasoningEfforts: []string{"adaptive"}}, {ID: "non-reasoning"}}}
	if task.AuthorizationMethod == "API_KEY" {
		observation.Source = "REMOTE_API"
	}
	changed := task
	changed.ClaimGeneration++
	if err := repository.CompleteProviderModelCatalogTask(ctx, changed, observation); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("changed claim: %v", err)
	}
	if err := repository.CompleteProviderModelCatalogTask(ctx, task, observation); err != nil {
		t.Fatalf("complete catalog: %v", err)
	}
	if err := repository.CompleteProviderModelCatalogTask(ctx, task, observation); err != nil {
		t.Fatalf("exact replay: %v", err)
	}
	if _, err := repository.ResolveProviderModelCatalogProof(ctx, principal); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("terminal proof: %v", err)
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	catalog, err := readModelCatalogTx(ctx, tx, scope{organizationID: task.OrganizationID}, task.ProviderDefinitionKey, task.AccountRef)
	if err != nil || catalog.Total != 3 || catalog.Status == nil || catalog.Status.State != "READY" || catalog.Status.ObservedAt == nil {
		t.Fatalf("account catalog readback: %+v %v", catalog, err)
	}
	for _, model := range catalog.Models {
		if !model.Available {
			t.Fatalf("observed model unavailable: %+v", model)
		}
	}
	observation.Models[0].DefaultReasoningEffort = "low"
	if err := repository.CompleteProviderModelCatalogTask(ctx, task, observation); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("changed terminal receipt: %v", err)
	}
	expired := task
	expired.Ref = "mcattsk_expiredfixture"
	expired.ExpiresAt = time.Now().Add(-time.Second).Truncate(time.Microsecond)
	expired.RequestDigest, err = encoder.ModelCatalogRequestDigest(expired)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.pool.Exec(ctx, queryCatalogFixtureExpiredTask, expired.Ref, expired.AuthorizationMethod, expired.ClaimantID, expired.ClaimGeneration, expired.Fence, expired.RequestDigest, expired.ExpiresAt, expired.OrganizationID, expired.AccountRef); err != nil {
		t.Fatal(err)
	}
	principal.RequestDigestSHA256 = expired.RequestDigest
	if _, err := repository.ResolveProviderModelCatalogProof(ctx, principal); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("expired proof: %v", err)
	}
	if err := repository.CompleteProviderModelCatalogTask(ctx, expired, observation); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("expired completion: %v", err)
	}
	if _, err := repository.pool.Exec(ctx, queryCatalogFixtureMutateClaim, expired.Ref); err == nil {
		t.Fatal("claimed input/lease was rewritten")
	}
	if _, err := repository.ClaimProviderModelCatalogTasks(ctx, "catalog-recovery", 1, encoder); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := repository.pool.QueryRow(ctx, queryCatalogFixtureTaskState, expired.Ref).Scan(&state); err != nil || state != "CANCELLED" {
		t.Fatalf("expired task not retired: %s %v", state, err)
	}
}

func seedObservedCatalogFixture(t *testing.T, ctx context.Context, repository *Repository, mutations ...func(*platformrepo.ProviderModelCatalogObservation)) {
	t.Helper()
	tasks, err := repository.ClaimProviderModelCatalogTasks(ctx, "catalog-seed-fixture", 16, &providercredentialclient.Client{})
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		source := "REMOTE_CODEX"
		if task.AuthorizationMethod == "API_KEY" {
			source = "REMOTE_API"
		}
		observation := platformrepo.ProviderModelCatalogObservation{AccountRef: task.AccountRef, CredentialRef: task.CredentialRef, Source: source, Failure: "NONE", ObservedAt: time.Now(), Models: []platformrepo.ProviderModelCatalogRecord{
			{ID: "gpt-5", DefaultReasoningEffort: "high", ReasoningEfforts: []string{"low", "medium", "high"}},
			{ID: "future-model", DefaultReasoningEffort: "adaptive", ReasoningEfforts: []string{"adaptive"}},
			{ID: "non-reasoning"},
		}}
		for _, mutate := range mutations {
			mutate(&observation)
		}
		if err := repository.CompleteProviderModelCatalogTask(ctx, task, observation); err != nil {
			t.Fatal(err)
		}
	}
}
