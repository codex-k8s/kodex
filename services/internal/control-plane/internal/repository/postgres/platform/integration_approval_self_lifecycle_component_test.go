package platform

import (
	"context"
	"errors"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
	"time"
)

// testIntegrationApprovalSelfLifecycle verifies self-only consent catalog, OCC/idempotent revoke, durable queued failure and short TTL expiry without claiming any external effect was cancelled.
// testIntegrationApprovalSelfLifecycle проверяет self-only consent catalog, OCC/idempotent revoke, durable queued failure и short TTL expiry, не объявляя внешние effects отменёнными.
func testIntegrationApprovalSelfLifecycle(t *testing.T, ctx context.Context, repository *Repository, pool *pgxpool.Pool, service *platformservice.Service, owner, viewer, gateway value.Principal, first, second, exhausted map[string]any, gateVersion int64) {
	t.Helper()
	self := owner
	self.Permission = "platform.integration-approval-consents.list"
	items, next, err := service.ListIntegrationApprovalConsents(ctx, self, query.Filter{Page: query.Page{Size: 1}})
	if err != nil || len(items) != 1 || next != "" || items[0].Lifetime != "INITIATOR_RESOURCE" || items[0].State != "EXHAUSTED" || items[0].MaxEffects != 100 || items[0].ScopeValuesJSON == "" {
		t.Fatal("self consent catalog lost immutable scope or bounds", err)
	}
	consent := items[0]
	foreign := viewer
	foreign.Permission = self.Permission
	if found, _, err := service.ListIntegrationApprovalConsents(ctx, foreign, query.Filter{}); err != nil || len(found) != 0 {
		t.Fatal("foreign initiator read consent", err)
	}
	foreign.Permission = "platform.integration-approval-consents.revoke"
	revoke := command.Command{Kind: command.RevokeIntegrationApprovalConsent, Principal: foreign, Mutation: value.Mutation{IdempotencyKey: "consent-foreign-revoke", ExpectedVersion: &consent.Version}, Payload: command.IntegrationApprovalConsentInput{ConsentRef: consent.Ref}}
	if _, err := service.Execute(ctx, revoke); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("foreign initiator revoked consent", err)
	}
	self.Permission = foreign.Permission
	revoke.Principal = self
	revoke.Mutation.IdempotencyKey = "consent-self-revoke"
	revoked, err := service.Execute(ctx, revoke)
	if err != nil || revoked.IntegrationApprovalConsent == nil || revoked.IntegrationApprovalConsent.State != "REVOKED" || revoked.IntegrationApprovalConsent.Version != consent.Version+1 {
		t.Fatal("self consent revoke failed", err)
	}
	replay, err := service.Execute(ctx, revoke)
	if err != nil || replay.IntegrationApprovalConsent == nil || replay.IntegrationApprovalConsent.Version != revoked.IntegrationApprovalConsent.Version {
		t.Fatal("consent revoke replay repeated transition", err)
	}
	revoke.Mutation.IdempotencyKey = "consent-stale-revoke"
	if _, err := service.Execute(ctx, revoke); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatal("consent stale version accepted", err)
	}
	claims, err := service.ClaimIntegrationInvocations(ctx, gateway, "consent-revoke-gateway", 10)
	if err != nil || len(claims) != 0 {
		t.Fatal("revoked queued effect claimed", err)
	}
	outcome, err := service.GetIntegrationInvocation(ctx, gateway, stringMap(second, "invocationRef"))
	if err != nil || stringMap(outcome, "state") != "FAILED" || stringMap(outcome, "safeErrorCode") != "INTEGRATION_APPROVAL_REVOKED" {
		t.Fatal("revoked READY effect hangs or has wrong outcome", err)
	}
	var events int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM control_plane.run_events WHERE safe_delta->>'IntegrationInvocationRef'=$1 AND type='TURN_PROGRESS' AND safe_summary='i18n:INTEGRATION_ACTION_FAILED'`, stringMap(second, "invocationRef")).Scan(&events); err != nil || events != 1 {
		t.Fatal("consent-expiry event cardinality changed", err)
	}
	approved, err := service.Execute(ctx, command.Command{Kind: command.ResolveOwnerGate, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "consent-short-approve", ExpectedVersion: &gateVersion}, Payload: command.GateResolutionInput{GateRef: stringMap(exhausted, "gateRef"), Decision: "APPROVE", IntegrationApproval: &command.IntegrationApprovalSelection{DurationSeconds: 1, MaxEffects: 1, Lifetime: "ROOT_RUN"}}})
	if err != nil || approved.Gate == nil || approved.Gate.State != "APPROVED" {
		t.Fatal("short consent not approved", err)
	}
	time.Sleep(1100 * time.Millisecond)
	claims, err = service.ClaimIntegrationInvocations(ctx, gateway, "consent-expiry-gateway", 10)
	if err != nil || len(claims) != 0 {
		t.Fatal("expired queued effect claimed", err)
	}
	outcome, err = service.GetIntegrationInvocation(ctx, gateway, stringMap(exhausted, "invocationRef"))
	if err != nil || stringMap(outcome, "state") != "FAILED" || stringMap(outcome, "safeErrorCode") != "INTEGRATION_APPROVAL_EXPIRED" {
		t.Fatal("expired READY effect hangs or has wrong outcome", err)
	}
	self.Permission = "platform.integration-approval-consents.list"
	catalog, _, err := service.ListIntegrationApprovalConsents(ctx, self, query.Filter{})
	if err != nil || len(catalog) != 2 {
		t.Fatal("consent durable catalog unavailable", err)
	}
	states := map[string]int{}
	for _, item := range catalog {
		states[item.State]++
	}
	if states["REVOKED"] != 1 || states["EXPIRED"] != 1 {
		t.Fatal("consent expiry/revoke states differ")
	}
}
