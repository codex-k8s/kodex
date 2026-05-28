package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunBuildsBackendPlanWithoutPrintingEnvValues(t *testing.T) {
	clearDeployPlanEnv(t)
	repoRoot := createDeployPlanRepo(t, true)
	envFile := filepath.Join(t.TempDir(), "config.env")
	if err := os.WriteFile(envFile, []byte(strings.Join([]string{
		"KODEX_INTERNAL_REGISTRY_HOST='secret.registry.local:5000'",
		"KODEX_PRODUCTION_NAMESPACE='secret-namespace'",
		"KODEX_PRODUCTION_DOMAIN='secret.example.test'",
		"KODEX_GITHUB_OAUTH_CLIENT_ID='secret-client'",
	}, "\n")), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	renderDir := filepath.Join(t.TempDir(), "rendered")

	var output bytes.Buffer
	if err := run(context.Background(), planOptions{
		RepoRoot:           repoRoot,
		EnvFilePath:        envFile,
		RenderDir:          renderDir,
		SkipLiveKubernetes: true,
	}, &output); err != nil {
		t.Fatalf("run deploy plan: %v", err)
	}

	for _, leaked := range []string{"secret.registry.local", "secret-namespace", "secret.example.test", "secret-client"} {
		if strings.Contains(output.String(), leaked) {
			t.Fatalf("deploy plan output leaked env value %q: %s", leaked, output.String())
		}
	}
	for _, expected := range []string{
		"PLAN: service access-manager",
		"PostgreSQL foundation rendered",
		"platform event-log migrations rendered",
		"backend service access-manager rendered",
		"live Kubernetes foundation checks skipped",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("deploy plan output missing %q: %s", expected, output.String())
		}
	}
	for _, path := range []string{
		filepath.Join(renderDir, "postgres", "kustomization.yaml"),
		filepath.Join(renderDir, "platform-event-log", "migrations.yaml"),
		filepath.Join(renderDir, "bootstrap-builder-smoke", "kaniko-smoke.yaml"),
		filepath.Join(renderDir, "access-manager", "access-manager.yaml"),
		filepath.Join(renderDir, "access-manager", "migrations.yaml"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected rendered file %s: %v", path, err)
		}
	}
}

func TestRunFailsWhenDeployableServiceImageIsMissing(t *testing.T) {
	clearDeployPlanEnv(t)
	repoRoot := createDeployPlanRepo(t, false)
	envFile := filepath.Join(t.TempDir(), "config.env")
	if err := os.WriteFile(envFile, []byte("KODEX_INTERNAL_REGISTRY_HOST='127.0.0.1:5000'\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	err := run(context.Background(), planOptions{
		RepoRoot:           repoRoot,
		EnvFilePath:        envFile,
		SkipLiveKubernetes: true,
	}, discardWriter{})
	if err == nil {
		t.Fatal("expected missing service image error")
	}
	if !strings.Contains(err.Error(), "resolve image access-manager") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func createDeployPlanRepo(t *testing.T, includeServiceImage bool) string {
	t.Helper()
	repoRoot := t.TempDir()
	serviceImage := ""
	if includeServiceImage {
		serviceImage = `
    access-manager:
      repository: '{{ envOr "KODEX_INTERNAL_REGISTRY_HOST" "127.0.0.1:5000" }}/kodex/access-manager'
      tagTemplate: '{{ version "access-manager" }}'
    access-manager-migrations:
      repository: '{{ envOr "KODEX_INTERNAL_REGISTRY_HOST" "127.0.0.1:5000" }}/kodex/access-manager-migrations'
      tagTemplate: '{{ version "access-manager" }}'
`
	}
	services := `spec:
  versions:
    access-manager:
      value: "0.1.0"
    registry:
      value: "2"
    kaniko-executor:
      value: "v1"
    crane:
      value: "debug"
    busybox:
      value: "1"
    pgvector:
      value: "pg16"
    platform-event-log:
      value: "0.1.0"
  images:
    postgres:
      from: 'pgvector/pgvector:{{ version "pgvector" }}'
    registry:
      from: 'registry:{{ version "registry" }}'
    kaniko-executor:
      from: 'kaniko:{{ version "kaniko-executor" }}'
    crane:
      from: 'crane:{{ version "crane" }}'
    busybox:
      from: 'busybox:{{ version "busybox" }}'
    platform-event-log-migrations:
      repository: '{{ envOr "KODEX_INTERNAL_REGISTRY_HOST" "127.0.0.1:5000" }}/kodex/platform-event-log-migrations'
      tagTemplate: '{{ version "platform-event-log" }}'
` + serviceImage + `  deployableServices:
    - name: access-manager
      status: foundation
      zone: internal
      path: services/internal/access-manager
      dockerfile: services/internal/access-manager/Dockerfile
      ownerDomain: access-and-accounts
      deploy:
        serviceManifest: deploy/base/access-manager/access-manager.yaml.tpl
        migrationsManifest: deploy/base/access-manager/migrations.yaml.tpl
        kustomization: deploy/base/access-manager/kustomization.yaml.tpl
`
	if err := os.WriteFile(filepath.Join(repoRoot, "services.yaml"), []byte(services), 0o644); err != nil {
		t.Fatalf("write services.yaml: %v", err)
	}
	writeFile(t, repoRoot, "services/internal/access-manager/Dockerfile", "FROM scratch\n")
	writeFile(t, repoRoot, "deploy/base/postgres/kustomization.yaml.tpl", "resources:\n  - postgres.yaml\n")
	writeFile(t, repoRoot, "deploy/base/postgres/postgres.yaml.tpl", "image: {{ image \"postgres\" }}\n")
	writeFile(t, repoRoot, "deploy/base/platform-event-log/migrations.yaml.tpl", "image: {{ image \"platform-event-log-migrations\" }}\n")
	writeFile(t, repoRoot, "deploy/base/bootstrap-foundation/kustomization.yaml.tpl", "resources:\n  - registry.yaml\n")
	writeFile(t, repoRoot, "deploy/base/bootstrap-foundation/registry.yaml.tpl", "image: {{ image \"registry\" }}\n")
	writeFile(t, repoRoot, "deploy/base/bootstrap-builder-smoke/kustomization.yaml.tpl", "resources:\n  - kaniko-smoke.yaml\n")
	writeFile(t, repoRoot, "deploy/base/bootstrap-builder-smoke/kaniko-smoke.yaml.tpl", "image: {{ image \"kaniko-executor\" }}\n")
	writeFile(t, repoRoot, "deploy/base/access-manager/kustomization.yaml.tpl", "resources:\n  - access-manager.yaml\n")
	writeFile(t, repoRoot, "deploy/base/access-manager/access-manager.yaml.tpl", "image: {{ image \"access-manager\" }}\n")
	writeFile(t, repoRoot, "deploy/base/access-manager/migrations.yaml.tpl", "image: {{ image \"access-manager-migrations\" }}\n")
	return repoRoot
}

func writeFile(t *testing.T, repoRoot string, path string, content string) {
	t.Helper()
	fullPath := filepath.Join(repoRoot, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("create dir for %s: %v", path, err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func clearDeployPlanEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"KODEX_INTERNAL_REGISTRY_HOST",
		"KODEX_PRODUCTION_NAMESPACE",
		"KODEX_PRODUCTION_DOMAIN",
		"KODEX_PUBLIC_BASE_URL",
		"KODEX_GITHUB_OAUTH_CLIENT_ID",
		"KODEX_GITHUB_OAUTH_CLIENT_SECRET",
	} {
		t.Setenv(name, "")
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) {
	return len(p), nil
}
