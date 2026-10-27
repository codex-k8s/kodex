package platform

import (
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// TestManagedMCPStartupRecoveryIsPrivateAndClosed rejects nil or partial recovery intents before
// database access and keeps private recovery distinct from terminal eligibility failure.
// TestManagedMCPStartupRecoveryIsPrivateAndClosed отклоняет отсутствующее либо частичное намерение
// восстановления до доступа к БД и отделяет закрытое восстановление от окончательной ошибки
// доступности кандидата.
func TestManagedMCPStartupRecoveryIsPrivateAndClosed(t *testing.T) {
	if _, err := enqueueManagedMCPStartupRecovery(t.Context(), nil, scope{}, nil); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("nil recovery intent accepted")
	}
	for _, grants := range [][]runtimecontract.RunnerIntegrationGrant{nil, {{DefinitionKey: "context7", CapabilityKey: runtimecontract.Context7QueryCapability}}} {
		if _, err := enqueueManagedMCPStartupRecovery(t.Context(), nil, scope{}, &managedMCPStartupRecovery{grants: grants}); !errors.Is(err, errs.ErrConflict) {
			t.Fatal("partial recovery pair reached database")
		}
	}
	if runtimeCandidateEligibilityFailure(&managedMCPStartupRecovery{}) {
		t.Fatal("private recovery became terminal eligibility error")
	}
}

// TestManagedMCPStartupRecoveryUsesOnlyStaleSuccessAndUnchangedPins checks stale-health SQL
// contains exact success/workload/generation/freshness/dependency guards and no INSERT or UPDATE;
// it does not execute recovery.
// TestManagedMCPStartupRecoveryUsesOnlyStaleSuccessAndUnchangedPins проверяет наличие точных
// ограничений успеха, workload, поколения, свежести и зависимостей в SQL старого здоровья и
// отсутствие INSERT и UPDATE; не выполняет восстановление.
func TestManagedMCPStartupRecoveryUsesOnlyStaleSuccessAndUnchangedPins(t *testing.T) {
	for _, required := range []string{"t.state='SUCCEEDED'", "t.claimed_workload='integration-gateway'", "t.generation>0", "INTERVAL '5 minutes'", "latest.completed_at DESC", "active.state IN ('DUE','CLAIMED')", "t.input_snapshot->'configuration'=c.public_configuration", "t.input_snapshot->>'credentialSHA256'=cr.content_sha256", "c.state='CONNECTED'", "c.version=@connection_version"} {
		if !strings.Contains(queryRuntimeManagedMCPStale, required) {
			t.Fatal("stale-only startup query lost guard")
		}
	}
	if strings.Contains(queryRuntimeManagedMCPStale, "UPDATE") || strings.Contains(queryRuntimeManagedMCPStale, "INSERT") {
		t.Fatal("classification mutated health history")
	}
}
