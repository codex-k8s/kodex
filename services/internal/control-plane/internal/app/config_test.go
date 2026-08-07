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
	if loaded.Revision != 24 {
		t.Fatalf("unexpected authority policy revision: %d", loaded.Revision)
	}
	for _, producerID := range []string{
		"control-plane.runtime-controller",
		"control-plane.runtime-restore-verifier",
		"control-plane.runtime-restore-effect",
		"control-plane.runtime-cleanup-authorizer",
		"control-plane.integration-gateway",
		"control-plane.integration-provider-readback",
		"control-plane.interaction-provider-readback",
		"control-plane.integration-continuation",
		"control-plane.agent-session",
		"control-plane.role-image-builder",
		"control-plane.image-admission",
		"control-plane.image-promotion",
		"control-plane.oidc",
	} {
		if _, ok := loaded.Producers[producerID]; !ok {
			t.Fatalf("required producer is absent: %s", producerID)
		}
	}
	securityBindings := map[string]string{
		"control.runtime-execution.restore.verify":      "runtime-restore-verifier",
		"control.runtime-execution.restore.bind":        "runtime-restore-verifier",
		"control.runtime-execution.rehydrate.complete":  "runtime-restore-verifier",
		"control.runtime-execution.restore.materialize": "runtime-controller",
		"control.runtime-execution.restore.credential":  "runtime-s3-restore-exchanger",
		"control.runtime-execution.cleanup.authorize":   "runtime-cleanup-authorizer",
		"control.runtime-execution.cleanup.expire":      "runtime-cleanup-authorizer",
		"control.runtime-execution.cleanup.consume":     "runtime-controller",
		"control.image-build.complete":                  "role-image-builder",
		"control.image-admission.record":                "image-admission",
		"control.image-promotion.claim":                 "image-promotion",
		"control.image-promotion.complete":              "image-promotion",
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

func TestWorkspaceRecoveryIntentDigestSeparatesTerminalOutcomes(t *testing.T) {
	t.Parallel()

	complete := workspaceRecoveryIntentDigest("11111111-1111-4111-8111-111111111111", 7, "complete", "")
	failed := workspaceRecoveryIntentDigest("11111111-1111-4111-8111-111111111111", 7, "fail", "recovery_readback_mismatch")
	if complete == failed || len(complete) != 64 || len(failed) != 64 {
		t.Fatalf("workspace recovery intent digests are not exact: %q %q", complete, failed)
	}
}
