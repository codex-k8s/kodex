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

// queryCatalogFixtureExpiredTask embeds creation of an already expired CLAIMED task from the exact
// fixture tenant/account current credential. It arranges a negative proof/completion case without
// issuing provider authority or contacting the provider.
// queryCatalogFixtureExpiredTask встраивает создание уже expired CLAIMED task из current
// credential точных fixture tenant/account. Готовит negative proof/completion case без выдачи
// provider authority и обращения к провайдеру.
//
//go:embed testdata/sql/model_catalog_insert_expired_task.sql
var queryCatalogFixtureExpiredTask string

// queryCatalogFixtureMutateClaim embeds intentional fence/expiry rewriting of a saved fixture
// task. The test expects immutable claim guards to reject the update; this SQL is not a supported
// renewal API.
// queryCatalogFixtureMutateClaim встраивает намеренное переписывание fence/expiry сохранённой
// fixture task. Тест ожидает отказа immutable claim guards; SQL не является поддержанным renewal
// API.
//
//go:embed testdata/sql/model_catalog_mutate_claim.sql
var queryCatalogFixtureMutateClaim string

// queryCatalogFixtureTaskState embeds fixture task-state readback by exact ref after claim
// maintenance. It observes stored retirement without proving that a remote probe stopped or any
// secret was revoked.
// queryCatalogFixtureTaskState встраивает fixture task-state readback по exact ref после claim
// maintenance. Наблюдает сохранённое завершение без доказательства остановки remote probe и отзыва
// секрета.
//
//go:embed testdata/sql/model_catalog_task_state.sql
var queryCatalogFixtureTaskState string

// queryCatalogFixtureCredentialID embeds internal credential-ID lookup by fixture credential ref
// for dependent component arrangements. It reads identity metadata only and does not retrieve
// credential values or grant materialization access.
// queryCatalogFixtureCredentialID встраивает чтение внутреннего credential ID по fixture
// credential ref для связанных component arrangements. Читает лишь identity metadata без
// credential values и выдачи materialization access.
//
//go:embed testdata/sql/model_catalog_credential_id.sql
var queryCatalogFixtureCredentialID string

// queryCatalogFixtureAdvanceAccount embeds exact tenant/account version increment used to
// invalidate a previously captured catalog tuple. It changes fixture OCC state without rotating a
// real credential or claiming refreshed observations.
// queryCatalogFixtureAdvanceAccount встраивает exact tenant/account version increment для
// инвалидации прежнего catalog tuple. Меняет fixture OCC state без ротации real credential и
// утверждения о fresh observations.
//
//go:embed testdata/sql/model_catalog_advance_account.sql
var queryCatalogFixtureAdvanceAccount string

// queryCatalogFixtureAccountID embeds identity lookup restricted to the exact fixture tenant/ref
// of an enabled AUTHORIZED account. It supplies an internal arrangement ID, not an authorization
// decision for an arbitrary caller.
// queryCatalogFixtureAccountID встраивает identity lookup лишь точных fixture tenant/ref enabled
// AUTHORIZED account. Возвращает внутренний arrangement ID, а не authorization decision
// произвольному caller.
//
//go:embed testdata/sql/model_catalog_account_id.sql
var queryCatalogFixtureAccountID string

// queryCatalogFixtureWarmAccount embeds the assistant runtime session account/ref/version read for
// one fixture organization. It verifies owner associations, not a healthy running Pod or
// successful provider connection.
// queryCatalogFixtureWarmAccount встраивает чтение account/ref/version сессии assistant runtime
// одной fixture organization. Проверяет owner associations, а не здоровый running Pod и успешное
// provider connection.
//
//go:embed testdata/sql/model_catalog_warm_account.sql
var queryCatalogFixtureWarmAccount string

// prepareObservedWarmFixture seeds synthetic owner catalog observations, resolves the
// runtime-controller warm permission and invokes domain warm reconciliation for the fixture. It
// advances owner readiness metadata without launching Kubernetes or performing an actual provider
// probe.
// prepareObservedWarmFixture заполняет synthetic owner catalog observations, разрешает
// runtime-controller warm permission и вызывает domain warm reconciliation fixture. Продвигает
// owner readiness metadata без запуска Kubernetes и настоящего provider probe.
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

// testCatalogOwnerProbe claims one real fixture owner task using a local digest encoder, checks
// exact/foreign proof, claim generation and terminal replay, and reads the three-model
// observation. It rejects changed receipt/expired work, tests immutable claim guards and
// maintenance cancellation. Observations are supplied synthetically; no remote API or Codex probe
// is executed.
// testCatalogOwnerProbe захватывает одну real fixture owner task с локальным digest encoder,
// проверяет exact/foreign proof, claim generation и terminal replay и читает three-model
// observation. Отклоняет changed receipt/expired work, проверяет immutable claim guards и
// maintenance cancellation. Observations передаются synthetic; remote API и Codex probe не
// исполняются.
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

// seedObservedCatalogFixture claims at most16 due fixture catalog tasks, supplies
// authorization-method-matched synthetic observations with three models and applies caller
// mutations before owner completion. Empty claims are allowed. It seeds storage state without
// reading secret values or verifying provider model availability.
// seedObservedCatalogFixture захватывает максимум16 due fixture catalog tasks, задаёт matching
// authorization method synthetic observations с тремя моделями и применяет caller mutations до
// owner completion. Пустой claims допустим. Заполняет storage state без чтения secret values и
// проверки provider model availability.
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
			{ID: "gpt-5", DefaultReasoningEffort: "high", ReasoningEfforts: []string{"low", "medium", "high"}, IsDefault: true},
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
