package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDefaultOperationBudgetMatchesDeployment preserves the shared Mattermost operation budget and binds the app default to the canonical ConfigMap used by both web profiles.
// TestDefaultOperationBudgetMatchesDeployment сохраняет общий Mattermost operation budget и связывает app default с canonical ConfigMap обоих web profiles.
func TestDefaultOperationBudgetMatchesDeployment(t *testing.T) {
	t.Setenv("DEPLOYMENT_ENVIRONMENT", "staging")
	t.Setenv("INTERACTION_GATEWAY_INSTANCE_ID", "default-budget-fixture")
	t.Setenv("INTERACTION_GATEWAY_OPERATION_TIMEOUT", "")
	config, err := loadConfig()
	if err != nil || config.OperationTimeout != 30*time.Second {
		t.Fatalf("default operation budget changed: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "deploy", "k8s", "base", "interaction-gateway", "configmap.yaml"))
	if err != nil || !strings.Contains(string(raw), "\n  INTERACTION_GATEWAY_OPERATION_TIMEOUT: "+config.OperationTimeout.String()+"\n") {
		t.Fatal("deployment operation budget differs from the application default", err)
	}
}
