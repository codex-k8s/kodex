package app

import (
	"path/filepath"
	"testing"

	authoritypolicy "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/authorization/policy"
)

func TestAuthorityPolicyMatchesEveryExpectedOperation(t *testing.T) {
	policyPath := filepath.Join(
		"..", "..", "..", "..", "..", "deploy", "k8s", "base",
		"internal-rpc-authority-publisher", "authority-policy.json",
	)
	loaded, err := authoritypolicy.Load(policyPath, expectedOperations())
	if err != nil {
		t.Fatalf("authority policy mismatch: %v", err)
	}
	if loaded.Revision != 7 {
		t.Fatalf("unexpected authority policy revision: %d", loaded.Revision)
	}
	for _, producerID := range []string{
		"control-plane.runtime-controller",
		"control-plane.integration-gateway",
		"control-plane.agent-session",
		"control-plane.oidc",
	} {
		if _, ok := loaded.Producers[producerID]; !ok {
			t.Fatalf("required producer is absent: %s", producerID)
		}
	}
}
