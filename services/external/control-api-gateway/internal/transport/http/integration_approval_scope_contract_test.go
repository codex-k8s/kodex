package httptransport

import (
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

// TestApprovalScopeContractPublicProjection preserves optional connection-only identity and exact bounded numbers without exposing internal authority prefixes.
// TestApprovalScopeContractPublicProjection сохраняет optional connection-only identity и exact bounded numbers без внутренних authority prefixes.
func TestApprovalScopeContractPublicProjection(t *testing.T) {
	contract := &cp.IntegrationApprovalScopeContract{Name: "Merge request", RequiredInputPaths: []string{"/project_id", "/merge_request_iid"}, MaxDurationSeconds: 604800, MaxEffects: 10000,
		AllowedLifetimes: []cp.IntegrationApprovalScopeLifetime{cp.IntegrationApprovalScopeLifetime_INTEGRATION_APPROVAL_SCOPE_LIFETIME_ROOT_RUN, cp.IntegrationApprovalScopeLifetime_INTEGRATION_APPROVAL_SCOPE_LIFETIME_INITIATOR_RESOURCE}}
	for _, paths := range [][]string{contract.RequiredInputPaths, {}} {
		contract.RequiredInputPaths = paths
		result, err := messageMap(contract)
		if err != nil || result["maxDurationSeconds"] != float64(604800) || result["maxEffects"] != float64(10000) {
			t.Fatal("approval metadata limits changed in HTTP projection", err)
		}
		projectedPaths, ok := result["requiredInputPaths"].([]any)
		if !ok || len(projectedPaths) != len(paths) {
			t.Fatal("empty required identity paths were lost")
		}
		choices, ok := result["allowedLifetimes"].([]any)
		if !ok || len(choices) != 2 || choices[0] != "ROOT_RUN" || choices[1] != "INITIATOR_RESOURCE" {
			t.Fatal("typed consent lifetime projection changed")
		}
	}
	for _, change := range []func(){
		func() { contract.MaxDurationSeconds = 0 },
		func() { contract.MaxDurationSeconds = 604801 },
		func() { contract.MaxEffects = 0 },
		func() { contract.MaxEffects = 10001 },
		func() { contract.AllowedLifetimes = nil },
		func() { contract.AllowedLifetimes = []cp.IntegrationApprovalScopeLifetime{0} },
	} {
		contract.MaxDurationSeconds, contract.MaxEffects = 604800, 10000
		contract.AllowedLifetimes = []cp.IntegrationApprovalScopeLifetime{cp.IntegrationApprovalScopeLifetime_INTEGRATION_APPROVAL_SCOPE_LIFETIME_ROOT_RUN}
		change()
		if _, err := messageMap(contract); err == nil {
			t.Fatal("malformed consent metadata became public configuration")
		}
	}
}
