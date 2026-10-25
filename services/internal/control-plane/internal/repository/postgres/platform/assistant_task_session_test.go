package platform

import (
	"errors"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"strings"
	"testing"
)

// TestAssistantTaskSessionCursorBindsFreshActorSourceAndOffset round-trips an exact cursor and
// checks foreign actor/scope gives Invalid, changed source VersionMismatch,
// malformed/noncanonical/zero/excessive offset fails and empty cursor starts page0. All pins are
// synthetic; no session read or access decision occurs.
// TestAssistantTaskSessionCursorBindsFreshActorSourceAndOffset проверяет round-trip exact cursor:
// foreign actor/scope даёт Invalid, changed source — VersionMismatch,
// malformed/noncanonical/zero/excessive offset отклоняется, пустой cursor начинает page0. Все pins
// synthetic; session read и access decision не выполняются.
func TestAssistantTaskSessionCursorBindsFreshActorSourceAndOffset(t *testing.T) {
	binding, source := strings.Repeat("a", 64), strings.Repeat("b", 64)
	token := encodeAssistantTaskSessionCursor(binding, source, 10)
	if offset, err := decodeAssistantTaskSessionCursor(token, binding, source); err != nil || offset != 10 {
		t.Fatal("exact cursor rejected")
	}
	if _, err := decodeAssistantTaskSessionCursor(token, strings.Repeat("c", 64), source); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("foreign actor/scope cursor accepted")
	}
	if _, err := decodeAssistantTaskSessionCursor(token, binding, strings.Repeat("c", 64)); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatal("changed source cursor accepted")
	}
	for _, bad := range []string{"?", token + "=", encodeAssistantTaskSessionCursor(binding, source, 10001), encodeAssistantTaskSessionCursor(binding, source, 0)} {
		if _, err := decodeAssistantTaskSessionCursor(bad, binding, source); err == nil {
			t.Fatal("malformed cursor accepted")
		}
	}
	if offset, err := decodeAssistantTaskSessionCursor("", binding, source); err != nil || offset != 0 {
		t.Fatal("first page rejected")
	}
}

// TestAssistantTaskSessionSQLKeepsCanonicalSourceEligibilityAndBudget checks both source/message
// SQL retain exact tenant/project/authority/visibility and message revision/session/turn/attempt
// lineage, newest-first LIMIT11 paging and absence of private payload fields. It asserts embedded
// SQL strings without testing PostgreSQL eligibility or event bytes.
// TestAssistantTaskSessionSQLKeepsCanonicalSourceEligibilityAndBudget проверяет exact
// tenant/project/authority/visibility в source/message SQL, message revision/session/turn/attempt
// lineage, newest-first LIMIT11 paging и отсутствие private payload fields. Проверяет embedded SQL
// strings без PostgreSQL eligibility и event bytes.
func TestAssistantTaskSessionSQLKeepsCanonicalSourceEligibilityAndBudget(t *testing.T) {
	for _, sql := range []string{queryAssistantTaskSessionSources, queryAssistantTaskSessionMessages} {
		for _, required := range []string{"anchor.organization_id=@organization_id::uuid", "source.organization_id=session.organization_id", "source.project_id IS NOT DISTINCT FROM session.project_id", "project.lifecycle='ACTIVE'", "@authority_project_id", "'run.view'", "catalog_resource_visible"} {
			if !strings.Contains(sql, required) {
				t.Fatal("source read lost canonical scope")
			}
		}
		for _, forbidden := range []string{"event.safe_delta,", "tool_call", "provider_credential", "safe_snapshot", "revision.instructions"} {
			if strings.Contains(sql, forbidden) {
				t.Fatal("read returned private material")
			}
		}
	}
	for _, required := range []string{"revision.attempt::text", "revision.session_id=session.id", "revision.turn_id=turn.id", "node.type='AGENT_EXECUTION'", "event.safe_delta->'Message'", "LIMIT 11 OFFSET @offset", "ORDER BY event.occurred_at DESC, event.ref DESC"} {
		if !strings.Contains(queryAssistantTaskSessionMessages, required) {
			t.Fatal("message read lost lineage or budget")
		}
	}
}
