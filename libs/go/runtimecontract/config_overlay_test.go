package runtimecontract

import (
	"strings"
	"testing"
)

func TestConfigOverlayStrictAllowlist(t *testing.T) {
	valid := "model_reasoning_effort = \"high\"\npersonality = \"pragmatic\"\nallow_login_shell = false\n\n[history]\npersistence = \"none\"\n"
	canonical, digest, err := CanonicalConfigOverlay(valid)
	if err != nil || canonical == "" || len(digest) != 64 {
		t.Fatalf("CanonicalConfigOverlay() = %q, %q, %v", canonical, digest, err)
	}
	for name, raw := range map[string]string{
		"credential": `openai_api_key = "value"`,
		"provider":   `model_provider = "attacker"`,
		"sandbox":    `sandbox_mode = "danger-full-access"`,
		"mcp":        `[mcp_servers.attacker]\nurl = "https://example.invalid"`,
		"login":      `allow_login_shell = true`,
		"syntax":     `history = [`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := CanonicalConfigOverlay(raw); err == nil {
				t.Fatalf("unsafe overlay accepted: %s", raw)
			}
		})
	}
}

func TestRuntimeEnvironmentRejectsReservedAndSecretValues(t *testing.T) {
	values := []RuntimeEnvironmentValue{{Name: "APP_MODE", Value: "test"}}
	secrets := []RuntimeSecretProjection{{Name: "CRM_TOKEN", SecretName: "runtime-crm-v1", SecretKey: "token",
		SecretUID: "7fe2f86e-4bb9-4325-a983-a389367c1cbf", SecretResourceVersion: "42", ContentSHA256: strings.Repeat("a", 64)}}
	digest, err := RuntimeEnvironmentDigest(values, secrets)
	if err != nil || len(digest) != 64 {
		t.Fatalf("RuntimeEnvironmentDigest() = %q, %v", digest, err)
	}
	values[0].Name = "OPENAI_API_KEY"
	if _, err := RuntimeEnvironmentDigest(values, secrets); err == nil {
		t.Fatal("reserved credential environment was accepted")
	}
	values[0].Name = "CRM_TOKEN"
	if _, err := RuntimeEnvironmentDigest(values, secrets); err == nil {
		t.Fatal("duplicated environment name was accepted")
	}
}
