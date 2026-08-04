package gateway

import (
	"testing"

	domainrepo "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/repository/gateway"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/entity"
)

func TestSemanticReceiptKeyIsExactToTurnAndGeneration(t *testing.T) {
	t.Parallel()
	scope := domainrepo.Scope{TenantID: "tenant", ProjectID: "project"}
	grant := entity.Grant{
		ID: "grant", ProcessID: "process", SessionID: "session", SessionVersion: 2,
		ThreadID: "thread", TurnID: "turn", TurnVersion: 3, Attempt: 1,
		InputDigest: "input", RuntimeRevisionID: "revision", RuntimeRevisionVersion: 4,
		RuntimeRevisionDigest: "revision-digest", RuntimeManifestDigest: "manifest-digest",
		RoleID: "role", RoleVersion: 5, Generation: 7,
	}
	first := semanticReceiptKey(scope, grant, "request")
	if first != semanticReceiptKey(scope, grant, "request") {
		t.Fatal("semantic receipt key is not deterministic")
	}
	changedTurn := grant
	changedTurn.TurnID = "other-turn"
	changedGeneration := grant
	changedGeneration.Generation++
	changedRuntimeDigest := grant
	changedRuntimeDigest.RuntimeRevisionDigest = "other-runtime-digest"
	if first == semanticReceiptKey(scope, changedTurn, "request") ||
		first == semanticReceiptKey(scope, changedGeneration, "request") ||
		first == semanticReceiptKey(scope, changedRuntimeDigest, "request") {
		t.Fatal("semantic receipt key does not bind exact turn and generation")
	}
}
