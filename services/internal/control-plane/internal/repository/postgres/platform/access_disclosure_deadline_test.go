package platform

import (
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/access"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestAccessDisclosureDeadlineUsesOnlyCurrentWinningAlternatives exercises the real allow-only evaluator before deadline selection, including group membership and future-starting grants.
// TestAccessDisclosureDeadlineUsesOnlyCurrentWinningAlternatives использует настоящий allow-only evaluator до выбора deadline, включая group membership и future-starting grants.
func TestAccessDisclosureDeadlineUsesOnlyCurrentWinningAlternatives(t *testing.T) {
	now := time.Now()
	end := now.Add(3 * time.Second).UTC()
	later := now.Add(7 * time.Second).UTC()
	future := now.Add(time.Second).UTC()
	expired := now.Add(-time.Second).UTC()
	equality := now.UTC()
	subject := entity.AccessSubject{Ref: "usr_fixture", Kind: "USER", Active: true, OIDCGroupRefs: []string{"group_fixture"}}
	target := entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: "int_fixture"}
	base := entity.AccessBinding{Ref: "ab_first", State: "ACTIVE", Subject: subject,
		Scope: entity.AccessScope{Kind: "ORGANIZATION"}, RoleVersion: entity.AccessRoleVersion{PermissionKeys: []string{"integration.view"}},
		Conditions: entity.AccessConditions{ValidUntil: &end}}
	second := base
	second.Ref, second.Conditions.ValidUntil = "ab_second", &later
	unbounded := base
	unbounded.Ref, unbounded.Conditions.ValidUntil = "ab_unbounded", nil
	futureUnbounded := unbounded
	futureUnbounded.Ref, futureUnbounded.Conditions.ValidFrom = "ab_future", &future
	past := base
	past.Ref, past.Conditions.ValidUntil = "ab_past", &expired
	equal := base
	equal.Ref, equal.Conditions.ValidUntil = "ab_equal", &equality
	group := second
	group.Ref, group.Subject = "ab_group", entity.AccessSubject{Ref: "group_fixture", Kind: "OIDC_GROUP", Active: true}
	foreignGroup := group
	foreignGroup.Ref, foreignGroup.Subject.Ref = "ab_foreign", "group_foreign"
	for _, test := range []struct {
		name     string
		bindings []entity.AccessBinding
		want     time.Time
		denied   bool
	}{
		{"finite", []entity.AccessBinding{base}, end, false},
		{"later alternative", []entity.AccessBinding{base, second}, later, false},
		{"current unbounded alternative", []entity.AccessBinding{base, unbounded}, time.Time{}, false},
		{"future unbounded never chains", []entity.AccessBinding{base, futureUnbounded}, end, false},
		{"future only denies", []entity.AccessBinding{futureUnbounded}, time.Time{}, true},
		{"expired only denies", []entity.AccessBinding{past}, time.Time{}, true},
		{"equality denies", []entity.AccessBinding{equal}, time.Time{}, true},
		{"actual group membership", []entity.AccessBinding{base, group}, later, false},
		{"foreign group never contributes", []entity.AccessBinding{base, foreignGroup}, end, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			decision := access.Evaluate(subject, "integration.view", target, subject.Ref, test.bindings, now.UTC())
			until, err := accessDisclosureDecisionUntil(decision, test.bindings, now)
			if test.denied {
				if !errors.Is(err, errs.ErrNotFound) || !until.IsZero() {
					t.Fatal("denied alternatives gained a disclosure allowance")
				}
				return
			}
			if err != nil || !until.Equal(test.want) {
				t.Fatal("winning alternatives changed the disclosure deadline", err)
			}
		})
	}
}

// TestAccessDisclosureDeadlineRejectsUnresolvedDecision prevents missing diagnostic provenance or a mismatched source set from becoming an unbounded allowance.
// TestAccessDisclosureDeadlineRejectsUnresolvedDecision не даёт отсутствующему provenance либо mismatched source set превратиться в unbounded allowance.
func TestAccessDisclosureDeadlineRejectsUnresolvedDecision(t *testing.T) {
	for _, explanation := range [][]entity.AccessExplanationStep{nil, {{BindingRef: "ab_missing"}}} {
		decision := entity.EffectiveAccessDecision{Allowed: true, Explanation: explanation}
		if until, err := accessDisclosureDecisionUntil(decision, nil, time.Now()); !errors.Is(err, errs.ErrUnavailable) || !until.IsZero() {
			t.Fatal("unresolved access decision became unbounded")
		}
	}
}
