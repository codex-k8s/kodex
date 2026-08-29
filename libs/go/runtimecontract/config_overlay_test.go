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
	image := testRuntimeEnvironmentImage("a")
	tools := []RuntimeEnvironmentTool{{Name: "GitHub CLI", Command: "gh", Description: "Работа с GitHub"}}
	digest, err := RuntimeEnvironmentDigest(values, secrets, image, tools)
	if err != nil || len(digest) != 64 {
		t.Fatalf("RuntimeEnvironmentDigest() = %q, %v", digest, err)
	}
	values[0].Name = "OPENAI_API_KEY"
	if _, err := RuntimeEnvironmentDigest(values, secrets, image, tools); err == nil {
		t.Fatal("reserved credential environment was accepted")
	}
	values[0].Name = "CRM_TOKEN"
	if _, err := RuntimeEnvironmentDigest(values, secrets, image, tools); err == nil {
		t.Fatal("duplicated environment name was accepted")
	}
}

func TestRuntimeEnvironmentDigestBindsExactImageAndNormalizedTools(t *testing.T) {
	values := []RuntimeEnvironmentValue{{Name: "APP_MODE", Value: "test"}}
	image := testRuntimeEnvironmentImage("a")
	tools := []RuntimeEnvironmentTool{
		{Name: "PostgreSQL CLI", Command: "psql", Description: "Диагностика PostgreSQL"},
		{Name: "GitHub CLI", Command: "gh", Description: "Работа с GitHub", UsageHint: "Используй gh api"},
	}
	baseline, err := RuntimeEnvironmentDigest(values, nil, image, tools)
	if err != nil {
		t.Fatalf("RuntimeEnvironmentDigest() error = %v", err)
	}
	reordered, err := RuntimeEnvironmentDigest(values, nil, image, []RuntimeEnvironmentTool{tools[1], tools[0]})
	if err != nil || reordered != baseline {
		t.Fatalf("tool order changed canonical digest: got=%q want=%q err=%v", reordered, baseline, err)
	}
	changedImage := testRuntimeEnvironmentImage("b")
	changedImageDigest, err := RuntimeEnvironmentDigest(values, nil, changedImage, tools)
	if err != nil || changedImageDigest == baseline {
		t.Fatalf("exact image change did not change digest: got=%q baseline=%q err=%v", changedImageDigest, baseline, err)
	}
	changedTools := append([]RuntimeEnvironmentTool(nil), tools...)
	changedTools[0].UsageHint = "Только read-only команды"
	changedToolsDigest, err := RuntimeEnvironmentDigest(values, nil, image, changedTools)
	if err != nil || changedToolsDigest == baseline {
		t.Fatalf("selected tools change did not change digest: got=%q baseline=%q err=%v", changedToolsDigest, baseline, err)
	}
	if _, err := RuntimeEnvironmentDigest(values, nil, RuntimeEnvironmentImage{}, tools); err == nil {
		t.Fatal("runtime environment without exact image was accepted")
	}
}

func testRuntimeEnvironmentImage(marker string) RuntimeEnvironmentImage {
	return RuntimeEnvironmentImage{
		ArtifactRef:      "imgart_abcdefgh" + marker,
		RecipeRef:        "imgrec_abcdefgh" + marker,
		RecipeGeneration: 1,
		Reference:        "registry.example/kodex/role@sha256:" + strings.Repeat(marker, 64),
		Digest:           "sha256:" + strings.Repeat(marker, 64),
	}
}
