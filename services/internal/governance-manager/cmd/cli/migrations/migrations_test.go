package migrations

import (
	"os"
	"strings"
	"testing"

	migrationtest "github.com/codex-k8s/kodex/libs/go/migrationtest"
)

func TestGooseMigrationFiles(t *testing.T) {
	t.Parallel()

	migrationtest.AssertGooseMigrationFiles(t, ".")
}

func TestSelfDeployPlanTargetTypeMigration(t *testing.T) {
	t.Parallel()

	contentBytes, err := os.ReadFile("20260609090000_governance_self_deploy_plan_target_type.sql")
	if err != nil {
		t.Fatalf("read self-deploy target migration: %v", err)
	}
	content := string(contentBytes)
	for _, expected := range []string{
		"governance_manager_risk_assessments_target_type_chk",
		"governance_manager_gate_requests_target_type_chk",
		"'self_deploy_plan'",
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("self-deploy target migration misses %q:\n%s", expected, content)
		}
	}
}
