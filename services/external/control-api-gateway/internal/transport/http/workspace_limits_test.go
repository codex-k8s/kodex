package httptransport

import (
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"testing"

	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// TestRuntimeWorkspaceLimitsHTTPMappingIsBoundedAndExplicit checks small and repository-sized overrides, excessive values and absence without silently selecting a quota.
// TestRuntimeWorkspaceLimitsHTTPMappingIsBoundedAndExplicit проверяет малые и репозиторные квоты, превышение границ и отсутствие без неявного назначения лимита.
func TestRuntimeWorkspaceLimitsHTTPMappingIsBoundedAndExplicit(t *testing.T) {
	input := generated.RuntimeEnvironmentPolicyInput{
		Resources:        generated.RuntimeResourcePolicy{WorkspaceLimits: &generated.RuntimeWorkspaceLimits{MaxBytes: 4096, MaxFiles: 10}},
		KubernetesAccess: generated.RuntimeKubernetesAccessKindNONE,
		WebAccess:        generated.RuntimeWebAccess{Mode: generated.RuntimeWebAccessModeNONE},
	}
	actual, ok := runtimeEnvironmentPolicyInput(input)
	if !ok || actual.Resources.WorkspaceLimits == nil || actual.Resources.WorkspaceLimits.MaxBytes != 4096 || actual.Resources.WorkspaceLimits.MaxFiles != 10 {
		t.Fatal("typed HTTP workspace budget lost")
	}
	input.Resources.WorkspaceLimits.MaxFiles = 50_000
	actual, ok = runtimeEnvironmentPolicyInput(input)
	if !ok || actual.Resources.WorkspaceLimits.MaxFiles != 50_000 {
		t.Fatal("explicit repository file quota rejected")
	}
	for _, limits := range []generated.RuntimeWorkspaceLimits{
		{MaxBytes: 0, MaxFiles: 10}, {MaxBytes: 1, MaxFiles: 0},
		{MaxBytes: 1<<30 + 1, MaxFiles: 10}, {MaxBytes: 1, MaxFiles: runtimecontract.RuntimeWorkspaceMaximumFiles + 1},
	} {
		input.Resources.WorkspaceLimits = &limits
		if _, ok := runtimeEnvironmentPolicyInput(input); ok {
			t.Fatal("invalid HTTP workspace budget accepted")
		}
	}
	input.Resources.WorkspaceLimits = nil
	actual, ok = runtimeEnvironmentPolicyInput(input)
	if !ok || actual.Resources.WorkspaceLimits != nil {
		t.Fatal("absent HTTP quota invented override")
	}
}
