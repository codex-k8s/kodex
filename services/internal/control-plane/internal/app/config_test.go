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
	if loaded.Revision != 8 {
		t.Fatalf("unexpected authority policy revision: %d", loaded.Revision)
	}
	for _, producerID := range []string{
		"control-plane.runtime-controller",
		"control-plane.runtime-restore-verifier",
		"control-plane.runtime-cleanup-authorizer",
		"control-plane.integration-gateway",
		"control-plane.agent-session",
		"control-plane.oidc",
	} {
		if _, ok := loaded.Producers[producerID]; !ok {
			t.Fatalf("required producer is absent: %s", producerID)
		}
	}
	securityBindings := map[string]string{
		"control.runtime-execution.restore.verify":    "runtime-restore-verifier",
		"control.runtime-execution.cleanup.authorize": "runtime-cleanup-authorizer",
		"control.runtime-execution.cleanup.expire":    "runtime-cleanup-authorizer",
		"control.runtime-execution.cleanup.consume":   "runtime-controller",
	}
	for operationID, workload := range securityBindings {
		operation, ok := loaded.Operations[operationID]
		if !ok || operation.CallerWorkload != workload {
			t.Fatalf("security operation %s has unexpected workload %q", operationID, operation.CallerWorkload)
		}
		if operation.CallerWorkload == "control-api-gateway" {
			t.Fatalf("control API gateway reached destructive operation %s", operationID)
		}
	}
}
