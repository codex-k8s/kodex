package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunPreflightRendersWithoutPrintingEnvValues(t *testing.T) {
	clearPreflightEnv(t)
	repoRoot := createPreflightRepo(t)
	envFile := filepath.Join(t.TempDir(), "config.env")
	if err := os.WriteFile(envFile, []byte("OPERATOR_USER='codex'\nKODEX_INTERNAL_REGISTRY_HOST='secret.registry.local:5000'\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	renderDir := filepath.Join(t.TempDir(), "rendered")

	var output bytes.Buffer
	if err := run(context.Background(), preflightOptions{
		RepoRoot:           repoRoot,
		EnvFilePath:        envFile,
		RenderDir:          renderDir,
		SkipLiveKubernetes: true,
	}, &output); err != nil {
		t.Fatalf("run preflight: %v", err)
	}

	if strings.Contains(output.String(), "secret.registry.local") {
		t.Fatalf("preflight output leaked env value: %s", output.String())
	}
	for _, path := range []string{
		filepath.Join(renderDir, "bootstrap-foundation", "kustomization.yaml"),
		filepath.Join(renderDir, "bootstrap-foundation", "registry.yaml"),
		filepath.Join(renderDir, "bootstrap-builder-smoke", "kustomization.yaml"),
		filepath.Join(renderDir, "bootstrap-builder-smoke", "kaniko-smoke.yaml"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected rendered file %s: %v", path, err)
		}
	}
}

func TestRunPreflightFailsWithoutOperatorUser(t *testing.T) {
	clearPreflightEnv(t)
	repoRoot := createPreflightRepo(t)
	envFile := filepath.Join(t.TempDir(), "config.env")
	if err := os.WriteFile(envFile, []byte("KODEX_INTERNAL_REGISTRY_HOST='127.0.0.1:5000'\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	err := run(context.Background(), preflightOptions{
		RepoRoot:           repoRoot,
		EnvFilePath:        envFile,
		SkipLiveKubernetes: true,
	}, ioDiscard{})
	if err == nil {
		t.Fatal("expected missing OPERATOR_USER error")
	}
	if !strings.Contains(err.Error(), "OPERATOR_USER is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func createPreflightRepo(t *testing.T) string {
	t.Helper()
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "services.yaml"), []byte(`spec:
  versions:
    registry:
      value: "2"
    kaniko-executor:
      value: "v1"
    crane:
      value: "debug"
    busybox:
      value: "1"
  images:
    registry:
      from: 'registry:{{ version "registry" }}'
    kaniko-executor:
      from: 'kaniko:{{ version "kaniko-executor" }}'
    crane:
      from: 'crane:{{ version "crane" }}'
    busybox:
      from: 'busybox:{{ version "busybox" }}'
`), 0o644); err != nil {
		t.Fatalf("write services.yaml: %v", err)
	}
	writeTemplate := func(path string, content string) {
		t.Helper()
		fullPath := filepath.Join(repoRoot, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("create dir for %s: %v", path, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	writeTemplate("deploy/base/bootstrap-foundation/kustomization.yaml.tpl", "resources:\n  - registry.yaml\n")
	writeTemplate("deploy/base/bootstrap-foundation/registry.yaml.tpl", "image: {{ imageOr \"registry\" \"KODEX_REGISTRY_IMAGE\" }}\n")
	writeTemplate("deploy/base/bootstrap-builder-smoke/kustomization.yaml.tpl", "resources:\n  - kaniko-smoke.yaml\n")
	writeTemplate("deploy/base/bootstrap-builder-smoke/kaniko-smoke.yaml.tpl", "image: {{ imageOr \"kaniko-executor\" \"KODEX_KANIKO_EXECUTOR_IMAGE\" }}\n")
	return repoRoot
}

func clearPreflightEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"OPERATOR_USER",
		"KODEX_INTERNAL_REGISTRY_HOST",
		"KODEX_INTERNAL_REGISTRY_PORT",
		"KODEX_ROLLOUT_TIMEOUT",
		"KODEX_KANIKO_TIMEOUT",
		"KODEX_BOOTSTRAP_SKIP_DNS_CHECK",
		"KODEX_FIREWALL_ENABLED",
		"KODEX_INGRESS_HOST_NETWORK",
		"KODEX_REGISTRY_IMAGE",
		"KODEX_KANIKO_EXECUTOR_IMAGE",
		"KODEX_IMAGE_MIRROR_TOOL_IMAGE",
		"KODEX_REGISTRY_SMOKE_SOURCE_IMAGE",
	} {
		t.Setenv(name, "")
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}
