package app

import "testing"

func TestManagementEffectsUseExactPlatformEgressContract(t *testing.T) {
	t.Parallel()

	if platformManagementEgressProxyURL != "http://egress-gateway.mattercodex-system.svc.cluster.local:8080" {
		t.Fatalf("unexpected platform egress URL: %q", platformManagementEgressProxyURL)
	}
	if managementEgressNoProxy != "localhost,127.0.0.1,::1,.svc,.svc.cluster.local" {
		t.Fatalf("unexpected management NO_PROXY: %q", managementEgressNoProxy)
	}
}
