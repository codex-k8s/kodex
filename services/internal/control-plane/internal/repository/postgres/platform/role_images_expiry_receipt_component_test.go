package platform

import (
	"context"
	_ "embed"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// queryRoleImageExpiryOutcomeReadback reads exact tenant/build expiry audit and actor/key claim
// receipt counts plus receipt type, alongside the fixture outbox total, for atomic
// negative-outcome and no-repeat assertions.
// queryRoleImageExpiryOutcomeReadback читает количества expiry audit точных tenant/build и claim
// receipt точных actor/key, а также тип квитанции и общий fixture outbox count для проверок
// атомарного отрицательного исхода и отсутствия повторных эффектов.
//
//go:embed testdata/sql/role_images_expiry_outcome_readback.sql
var queryRoleImageExpiryOutcomeReadback string

// testRoleImageExpiryOutcome runs the same expiry-only claim receipt contract for organization or
// project ownership in the caller isolated suite. It requires atomic audit/event/negative receipt,
// repeated and concurrent NotFound without taking new work, fresh-key progress, no receipt for
// true idle and rejection of renewal with the old terminal lease.
// testRoleImageExpiryOutcome проверяет один контракт expiry-only claim receipt для organization
// либо project ownership в изолированном suite вызывающей стороны. Требует атомарные
// audit/event/negative receipt, повторный и конкурентный NotFound без захвата новой работы, работу
// со свежим key, отсутствие receipt при настоящем idle и отказ renew со старым terminal lease.
func testRoleImageExpiryOutcome(t *testing.T, ctx context.Context, repository *Repository,
	owner, uiOwner, worker value.Principal, projectRef string,
) {
	t.Helper()
	label := "organization"
	manage := repository.ManageOrganization
	roleRef := ""
	if projectRef != "" {
		label, manage = "project", repository.Manage
		service, err := platformservice.New(repository)
		if err != nil {
			t.Fatal(err)
		}
		agent := createLifecycleAgent(t, ctx, service, uiOwner, projectRef, "expiry-project-agent", "Expiry receipt specialist")
		roleRef = agent.RoleDefinitionRef
	}
	_, specification := promotionComponentCatalog(t)
	create := func(suffix string) roleimagerepo.ManageResult {
		t.Helper()
		result, err := manage(ctx, roleimagerepo.ManageInput{Principal: owner, ProjectRef: projectRef, RoleDefinitionRef: roleRef, Action: "CREATE",
			Name: "Expiry receipt " + label + suffix, Recipe: specification, Mutation: roleImageTestMutation("expiry-"+label+suffix, "CREATE", nil)})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	terminal := create("-terminal")
	claim, err := repository.ClaimBuild(ctx, worker, "expiry-"+label+"-initial")
	if err != nil || claim.Build.Ref != terminal.Build.Ref {
		t.Fatalf("claim expiry fixture: %v", err)
	}
	if _, err := repository.pool.Exec(ctx, queryOrganizationImageComponentTerminalExpiry, claim.Build.Ref); err != nil {
		t.Fatal(err)
	}
	current, err := repository.resolveScope(ctx, worker)
	if err != nil {
		t.Fatal(err)
	}
	key := "expiry-" + label + "-empty-outcome"
	read := func() (int, int, int, string) {
		t.Helper()
		var audits, events, receipts int
		var kind string
		if err := repository.pool.QueryRow(ctx, queryRoleImageExpiryOutcomeReadback, current.organizationID, claim.Build.Ref, current.actorID, key).Scan(&audits, &events, &receipts, &kind); err != nil {
			t.Fatal(err)
		}
		return audits, events, receipts, kind
	}
	audits, events, receipts, _ := read()
	if _, err := repository.ClaimBuild(ctx, worker, key); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("expiry-only claim outcome: %v", err)
	}
	nextAudits, nextEvents, nextReceipts, kind := read()
	if nextAudits != audits+1 || nextEvents != events+1 || nextReceipts != receipts+1 || kind != "IMAGE_BUILD_EXPIRY_OUTCOME" {
		t.Fatal("expiry audit/event/negative receipt were not committed atomically")
	}
	fresh := create("-fresh")
	nextAudits, nextEvents, nextReceipts, _ = read()
	if _, err := repository.ClaimBuild(ctx, worker, key); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("negative claim replay took new work: %v", err)
	}
	gotAudits, gotEvents, gotReceipts, _ := read()
	if gotAudits != nextAudits || gotEvents != nextEvents || gotReceipts != nextReceipts {
		t.Fatal("negative claim replay created side effects")
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := repository.ClaimBuild(ctx, worker, key)
			results <- err
		}()
	}
	close(start)
	for range 2 {
		if err := <-results; !errors.Is(err, errs.ErrNotFound) {
			t.Fatalf("concurrent negative outcome replay: %v", err)
		}
	}
	gotAudits, gotEvents, gotReceipts, _ = read()
	if gotAudits != nextAudits || gotEvents != nextEvents || gotReceipts != nextReceipts {
		t.Fatal("concurrent replay changed expiry outcome")
	}
	claimed, err := repository.ClaimBuild(ctx, worker, key+"-new")
	if err != nil || claimed.Build.Ref != fresh.Build.Ref || claimed.Build.Attempt != 1 {
		t.Fatalf("fresh key did not claim untouched new work: %v", err)
	}
	version := int64(fresh.Recipe.Version)
	if _, err := manage(ctx, roleimagerepo.ManageInput{Principal: owner, ProjectRef: projectRef, Action: "CANCEL_BUILD",
		RecipeRef: fresh.Recipe.Ref, BuildRef: claimed.Build.Ref, Mutation: roleImageTestMutation(key+"-cancel", "CANCEL_BUILD", &version)}); err != nil {
		t.Fatal(err)
	}
	idle := key + "-idle"
	if _, err := repository.ClaimBuild(ctx, worker, idle); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("true idle claim: %v", err)
	}
	var idleReceipts int
	var idleAudits, idleEvents int
	var idleType string
	if err := repository.pool.QueryRow(ctx, queryRoleImageExpiryOutcomeReadback, current.organizationID, claim.Build.Ref, current.actorID, idle).Scan(&idleAudits, &idleEvents, &idleReceipts, &idleType); err != nil || idleReceipts != 0 || idleType != "" {
		t.Fatal("true idle unexpectedly persisted an outcome")
	}
	// Terminal grant закрыт: прежний lease не разрешает heartbeat.
	if _, err := repository.RenewBuild(ctx, roleimagerepo.BuildLeaseInput{Principal: worker, IdempotencyKey: key + "-late",
		BuildRef: claim.Build.Ref, LeaseToken: claim.LeaseToken, ExpectedVersion: claim.Build.Version,
		ExpectedAttempt: claim.Build.Attempt, ExpectedFence: claim.Fence}); !errors.Is(err, errs.ErrForbidden) && !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("expired lease accepted renewal: %v", err)
	}
}
