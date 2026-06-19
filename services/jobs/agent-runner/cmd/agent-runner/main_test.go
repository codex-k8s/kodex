package main

import (
	"strings"
	"testing"
)

func TestCodexShellEnvironmentAllowlistIncludesRuntimeEnv(t *testing.T) {
	t.Setenv("MATTERCODEX_RUNTIME_ENV_ALLOWLIST", "RADAR_AUTO_KUBECONFIG,invalid-name,STAGING_DB_URL,RADAR_AUTO_KUBECONFIG")

	values := codexShellEnvironmentAllowlist()
	joined := "," + strings.Join(values, ",") + ","

	for _, expected := range []string{"RADAR_AUTO_KUBECONFIG", "STAGING_DB_URL", "MATTERCODEX_MCP_TOKEN"} {
		if !strings.Contains(joined, ","+expected+",") {
			t.Fatalf("allowlist missing %q: %#v", expected, values)
		}
	}
	if strings.Contains(joined, ",invalid-name,") {
		t.Fatalf("allowlist contains invalid env name: %#v", values)
	}
	if strings.Count(joined, ",RADAR_AUTO_KUBECONFIG,") != 1 {
		t.Fatalf("allowlist contains duplicate runtime env: %#v", values)
	}
}
