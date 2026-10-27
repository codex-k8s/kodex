package platform

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

// TestCapabilityAuthorityUsesExactCurrentBindings checks active group bindings authorize only the
// exact target agent and stop at expiry or revocation without escalating other capabilities.
// TestCapabilityAuthorityUsesExactCurrentBindings проверяет полномочия активных групповых привязок
// только для точного целевого агента и их прекращение при истечении или отзыве без расширения иных
// capability.
func TestCapabilityAuthorityUsesExactCurrentBindings(t *testing.T) {
	now := time.Now().UTC()
	until := now.Add(time.Minute)
	subject := entity.AccessSubject{Ref: "usr_actor", Kind: "USER", Active: true, OIDCGroupRefs: []string{"grp_launcher"}}
	authority := capabilityAuthority{subject: subject, projectRef: "prj_a", agentRef: "agt_a", organizationRef: "org_a", evaluatedAt: now,
		bindings: []entity.AccessBinding{{Ref: "bind_a", State: "ACTIVE", Subject: entity.AccessSubject{Ref: "grp_launcher", Kind: "OIDC_GROUP", Active: true},
			RoleVersion: entity.AccessRoleVersion{PermissionKeys: []string{"agent.launch"}},
			Scope:       entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: "prj_a", ResourceKind: "AGENT", ResourceRef: "agt_a"},
			Conditions:  entity.AccessConditions{ValidUntil: &until}}}}
	if !authority.platformAllowed("platform.run.launch") || !authority.platformAllowed("platform.run.delegate") {
		t.Fatal("current group permission did not enable the exact agent capability")
	}
	for _, key := range []string{"platform.agent.manage", "platform.project.manage", "platform.stt.use", "unknown"} {
		if authority.platformAllowed(key) {
			t.Fatalf("launcher escalated capability %s", key)
		}
	}
	authority.agentRef = "agt_b"
	if authority.platformAllowed("platform.run.launch") {
		t.Fatal("exact binding escaped to another agent")
	}
	authority.agentRef = "agt_a"
	authority.evaluatedAt = until
	if authority.platformAllowed("platform.run.launch") {
		t.Fatal("expired binding enabled a fresh runtime")
	}
	authority.evaluatedAt = now
	authority.bindings[0].State = "REVOKED"
	if authority.platformAllowed("platform.run.launch") {
		t.Fatal("revoked binding enabled a fresh runtime")
	}
}

// TestIntegrationCapabilityAuthoritySeparatesConnectionsAndRisk confines integration.view to the
// exact connection and READ risk, rejecting other connections and write, sensitive or unknown
// risk.
// TestIntegrationCapabilityAuthoritySeparatesConnectionsAndRisk ограничивает integration.view
// точным соединением и риском READ, отклоняя чужие соединения и риски записи, чувствительных либо
// неизвестных операций.
func TestIntegrationCapabilityAuthoritySeparatesConnectionsAndRisk(t *testing.T) {
	subject := entity.AccessSubject{Ref: "usr_actor", Kind: "USER", Active: true}
	target := entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: "int_allowed"}
	authority := capabilityAuthority{subject: subject, evaluatedAt: time.Now().UTC(), bindings: []entity.AccessBinding{{State: "ACTIVE", Subject: subject,
		RoleVersion: entity.AccessRoleVersion{PermissionKeys: []string{"integration.view"}}, Scope: target}}}
	capability := integrationpackage.Capability{Key: "integration.read", Risk: "READ"}
	if !integrationCapabilityAllowed(authority, capability, target) {
		t.Fatal("exact read permission was rejected")
	}
	target.ResourceRef = "int_denied"
	if integrationCapabilityAllowed(authority, capability, target) {
		t.Fatal("same capability key escaped its connection boundary")
	}
	target.ResourceRef = "int_allowed"
	for _, risk := range []string{"WRITE", "SENSITIVE", "unknown", ""} {
		capability.Risk = risk
		if integrationCapabilityAllowed(authority, capability, target) {
			t.Fatal("read permission escalated to a write capability")
		}
	}
}

// TestEffectiveCapabilityProjectionCursorIsActorAndSnapshotBound checks sorted paging, stable
// digest across read clocks, revoked eligibility invalidation, tenant/actor/project binding and
// server-side search.
// TestEffectiveCapabilityProjectionCursorIsActorAndSnapshotBound проверяет сортированную
// пагинацию, устойчивый дайджест при смене времени чтения, аннулирование при отзыве доступности,
// привязку tenant, actor и проекта и серверный поиск.
func TestEffectiveCapabilityProjectionCursorIsActorAndSnapshotBound(t *testing.T) {
	current := scope{organizationID: "org_a", actorID: "actor_a"}
	fresh := func() entity.AgentEffectiveCapabilities {
		return entity.AgentEffectiveCapabilities{AgentRef: "agt_a", AgentVersion: 4, RuntimeConfigurationVersion: 2, EvaluatedAt: time.Now().UTC(),
			Items: []entity.EffectiveCapability{{Key: "b", Reason: capabilityActorDenied}, {Key: "a", Requested: true, Effective: true, Reason: capabilityAvailable}, {Key: "c", Name: "Поиск"}}}
	}
	first := fresh()
	if err := pageEffectiveCapabilities(&first, current, query.Filter{Page: query.Page{Size: 1}}); err != nil || first.Total != 3 || len(first.Items) != 1 || first.Items[0].Key != "a" || first.NextPageToken == "" {
		t.Fatalf("first page is invalid: total=%d err=%v", first.Total, err)
	}
	filter := query.Filter{Page: query.Page{Size: 1, Token: first.NextPageToken}}
	second := fresh()
	if err := pageEffectiveCapabilities(&second, current, filter); err != nil || second.Digest != first.Digest || second.Items[0].Key != "b" || second.Total != 3 {
		t.Fatalf("fresh evaluation time invalidated unchanged snapshot: %v", err)
	}
	changed := fresh()
	changed.Items[1].Effective = false
	changed.Items[1].Reason = capabilityActorDenied
	if err := pageEffectiveCapabilities(&changed, current, filter); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("revoked eligibility reused old cursor: %v", err)
	}
	for name, altered := range map[string]scope{"actor": {organizationID: "org_a", actorID: "actor_b"}, "tenant": {organizationID: "org_b", actorID: "actor_a"}, "project": {organizationID: "org_a", actorID: "actor_a", authorityProjectID: "prj_b"}} {
		result := fresh()
		if err := pageEffectiveCapabilities(&result, altered, filter); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("%s cursor was accepted: %v", name, err)
		}
	}
	for _, token := range []string{"invalid", strings.Repeat("a", 2049)} {
		result := fresh()
		if err := pageEffectiveCapabilities(&result, current, query.Filter{Page: query.Page{Token: token}}); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("malformed cursor was accepted: %v", err)
		}
	}
	result := fresh()
	if err := pageEffectiveCapabilities(&result, current, query.Filter{Query: "поиск"}); err != nil || result.Total != 1 || result.Items[0].Key != "c" {
		t.Fatalf("server query failed: %v", err)
	}
}

// TestEffectiveCapabilityReasonsPreserveIntersection checks reason precedence across request,
// actor authority, runtime readiness and workflow exclusion with required capabilities.
// TestEffectiveCapabilityReasonsPreserveIntersection проверяет приоритет причин по запросу
// capability, полномочиям actor, готовности runtime и исключению workflow с учётом обязательных
// capability.
func TestEffectiveCapabilityReasonsPreserveIntersection(t *testing.T) {
	for _, test := range []struct {
		requested, allowed, ready, workflow, required bool
		reason                                        string
	}{
		{true, true, true, false, false, capabilityAvailable},
		{true, false, true, false, false, capabilityActorDenied},
		{false, true, true, false, false, capabilityNotRequested},
		{true, true, false, false, false, capabilityRuntimeNotReady},
		{true, true, true, true, false, capabilityWorkflowExcluded},
		{true, true, true, true, true, capabilityAvailable},
	} {
		if actual := effectiveCapabilityReason(test.requested, test.allowed, test.ready, test.workflow, test.required); actual != test.reason {
			t.Fatalf("capability reason: got %s want %s", actual, test.reason)
		}
	}
}
