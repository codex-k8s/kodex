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
		"KODEX_GITHUB_OAUTH_CLIENT_SECRET='secret-client-secret'",
		"KODEX_POSTGRES_PASSWORD='secret-postgres-password'",
		"KODEX_ACCESS_MANAGER_DATABASE_DSN='secret-access-dsn'",
		"KODEX_ACCESS_MANAGER_GRPC_AUTH_TOKEN='secret-access-token'",
	}, "\n")+"\n"+strings.Join(selfDeployReadinessEnvLines(), "\n")), 0o600); err != nil {
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

	for _, leaked := range []string{"secret.registry.local", "secret-namespace", "secret.example.test", "secret-client", "secret-client-secret", "secret-postgres-password", "secret-access-dsn", "secret-access-token"} {
		if strings.Contains(output.String(), leaked) {
			t.Fatalf("deploy plan output leaked env value %q: %s", leaked, output.String())
		}
	}
	for _, expected := range []string{
		"PLAN: service access-manager",
		"PostgreSQL foundation rendered",
		"platform event-log migrations rendered",
		"backend service access-manager rendered",
		"backend service agent-manager rendered",
		"agent-manager self-deploy rendered manifest surface checked",
		"live Kubernetes foundation checks skipped",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("deploy plan output missing %q: %s", expected, output.String())
		}
	}
	for _, path := range []string{
		filepath.Join(renderDir, "postgres", "kustomization.yaml"),
		filepath.Join(renderDir, "postgres", "secrets.yaml"),
		filepath.Join(renderDir, "platform-event-log", "migrations.yaml"),
		filepath.Join(renderDir, "bootstrap-builder-smoke", "kaniko-smoke.yaml"),
		filepath.Join(renderDir, "access-manager", "access-manager.yaml"),
		filepath.Join(renderDir, "access-manager", "migrations.yaml"),
		filepath.Join(renderDir, "agent-manager", "agent-manager.yaml"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected rendered file %s: %v", path, err)
		}
	}
	assertRenderedTreeDoesNotContain(t, renderDir, "secret-postgres-password", "secret-access-dsn", "secret-access-token", "secret.registry.local", "secret-namespace", "secret-client-secret")
}

func TestRunFailsWhenDeployableServiceImageIsMissing(t *testing.T) {
	clearDeployPlanEnv(t)
	repoRoot := createDeployPlanRepo(t, false)
	envFile := filepath.Join(t.TempDir(), "config.env")
	if err := os.WriteFile(envFile, []byte("KODEX_INTERNAL_REGISTRY_HOST='127.0.0.1:5000'\n"+strings.Join(selfDeployReadinessEnvLines(), "\n")), 0o600); err != nil {
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

func TestRunFailsWhenSelfDeployProjectIDIsMissing(t *testing.T) {
	clearDeployPlanEnv(t)
	repoRoot := createDeployPlanRepo(t, true)
	envFile := filepath.Join(t.TempDir(), "config.env")
	if err := os.WriteFile(envFile, []byte(strings.Join([]string{
		"KODEX_INTERNAL_REGISTRY_HOST='127.0.0.1:5000'",
		"KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_CONSUMER_ENABLED='true'",
		"KODEX_AGENT_MANAGER_SELF_DEPLOY_GOVERNANCE_GATE_ENABLED='true'",
		"KODEX_AGENT_MANAGER_SELF_DEPLOY_BUILD_DISPATCH_ENABLED='true'",
	}, "\n")), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	err := run(context.Background(), planOptions{
		RepoRoot:           repoRoot,
		EnvFilePath:        envFile,
		SkipLiveKubernetes: true,
	}, discardWriter{})
	if err == nil {
		t.Fatal("expected missing self-deploy project id error")
	}
	if !strings.Contains(err.Error(), "KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_PROJECT_ID") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunFailsWhenSelfDeployBuildDispatchIsDisabled(t *testing.T) {
	clearDeployPlanEnv(t)
	repoRoot := createDeployPlanRepo(t, true)
	envFile := filepath.Join(t.TempDir(), "config.env")
	if err := os.WriteFile(envFile, []byte(strings.Join([]string{
		"KODEX_INTERNAL_REGISTRY_HOST='127.0.0.1:5000'",
		"KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_PROJECT_ID='63135040-fe44-4ec4-83d5-b0126dc23b32'",
		"KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_CONSUMER_ENABLED='true'",
		"KODEX_AGENT_MANAGER_SELF_DEPLOY_GOVERNANCE_GATE_ENABLED='true'",
		"KODEX_AGENT_MANAGER_SELF_DEPLOY_BUILD_DISPATCH_ENABLED='false'",
	}, "\n")), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	err := run(context.Background(), planOptions{
		RepoRoot:           repoRoot,
		EnvFilePath:        envFile,
		SkipLiveKubernetes: true,
	}, discardWriter{})
	if err == nil {
		t.Fatal("expected disabled self-deploy build dispatch error")
	}
	if !strings.Contains(err.Error(), "KODEX_AGENT_MANAGER_SELF_DEPLOY_BUILD_DISPATCH_ENABLED") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckRenderedSelfDeploySurfaceRequiresAgentManagerEnv(t *testing.T) {
	renderDir := t.TempDir()
	manifestDir := filepath.Join(renderDir, "agent-manager")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatalf("create rendered manifest dir: %v", err)
	}
	content := strings.Join(requiredSelfDeployRenderedManifestFragments(), "\n")
	if err := os.WriteFile(filepath.Join(manifestDir, "agent-manager.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write rendered manifest: %v", err)
	}
	var output bytes.Buffer
	if err := checkRenderedSelfDeploySurface(renderDir, &output); err != nil {
		t.Fatalf("check rendered self-deploy surface: %v", err)
	}

	missing := "KODEX_AGENT_MANAGER_SELF_DEPLOY_BUILD_DISPATCH_ENABLED"
	content = strings.ReplaceAll(content, missing, "")
	if err := os.WriteFile(filepath.Join(manifestDir, "agent-manager.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("rewrite rendered manifest: %v", err)
	}
	err := checkRenderedSelfDeploySurface(renderDir, discardWriter{})
	if err == nil {
		t.Fatal("expected missing rendered self-deploy surface error")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRequiredSelfDeployRuntimeSecretKeys(t *testing.T) {
	keys := strings.Join(requiredSelfDeployRuntimeSecretKeys(), "\n")
	for _, expected := range []string{
		"KODEX_AGENT_MANAGER_PROJECT_CATALOG_GRPC_AUTH_TOKEN",
		"KODEX_AGENT_MANAGER_RUNTIME_MANAGER_GRPC_AUTH_TOKEN",
		"KODEX_AGENT_MANAGER_GOVERNANCE_MANAGER_GRPC_AUTH_TOKEN",
		"KODEX_AGENT_MANAGER_EVENT_LOG_DATABASE_DSN",
	} {
		if !strings.Contains(keys, expected) {
			t.Fatalf("required self-deploy runtime secret keys missing %s: %s", expected, keys)
		}
	}
}

func selfDeployReadinessEnvLines() []string {
	return []string{
		"KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_PROJECT_ID='63135040-fe44-4ec4-83d5-b0126dc23b32'",
		"KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_CONSUMER_ENABLED='true'",
		"KODEX_AGENT_MANAGER_SELF_DEPLOY_GOVERNANCE_GATE_ENABLED='true'",
		"KODEX_AGENT_MANAGER_SELF_DEPLOY_BUILD_DISPATCH_ENABLED='true'",
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
    agent-manager:
      repository: '{{ envOr "KODEX_INTERNAL_REGISTRY_HOST" "127.0.0.1:5000" }}/kodex/agent-manager'
      tagTemplate: '{{ version "agent-manager" }}'
`
	}
	services := `spec:
  versions:
    access-manager:
      value: "0.1.0"
    agent-manager:
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
    traefik:
      value: "v3"
    oauth2-proxy:
      value: "v7"
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
    traefik:
      from: 'traefik:{{ version "traefik" }}'
    oauth2-proxy:
      from: 'oauth2-proxy:{{ version "oauth2-proxy" }}'
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
    - name: agent-manager
      status: foundation
      zone: internal
      path: services/internal/agent-manager
      dockerfile: services/internal/agent-manager/Dockerfile
      ownerDomain: agent-orchestration
      deploy:
        serviceManifest: deploy/base/agent-manager/agent-manager.yaml.tpl
        kustomization: deploy/base/agent-manager/kustomization.yaml.tpl
`
	if err := os.WriteFile(filepath.Join(repoRoot, "services.yaml"), []byte(services), 0o644); err != nil {
		t.Fatalf("write services.yaml: %v", err)
	}
	writeFile(t, repoRoot, "services/internal/access-manager/Dockerfile", "FROM scratch\n")
	writeFile(t, repoRoot, "services/internal/agent-manager/Dockerfile", "FROM scratch\n")
	writeFile(t, repoRoot, "deploy/base/postgres/kustomization.yaml.tpl", "resources:\n  - secrets.yaml\n  - postgres.yaml\n")
	writeFile(t, repoRoot, "deploy/base/postgres/secrets.yaml.tpl", `apiVersion: v1
kind: Secret
metadata:
  name: kodex-postgres
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "" }}
type: Opaque
stringData:
  KODEX_POSTGRES_PASSWORD: "{{ envOr "KODEX_POSTGRES_PASSWORD" "" }}"
  KODEX_ACCESS_MANAGER_DATABASE_DSN: "{{ envOr "KODEX_ACCESS_MANAGER_DATABASE_DSN" "" }}"
  KODEX_ACCESS_MANAGER_GRPC_AUTH_TOKEN: "{{ envOr "KODEX_ACCESS_MANAGER_GRPC_AUTH_TOKEN" "" }}"
`)
	writeFile(t, repoRoot, "deploy/base/postgres/postgres.yaml.tpl", "image: {{ image \"postgres\" }}\n")
	writeFile(t, repoRoot, "deploy/base/platform-event-log/migrations.yaml.tpl", "image: {{ image \"platform-event-log-migrations\" }}\n")
	writeFile(t, repoRoot, "deploy/base/bootstrap-foundation/kustomization.yaml.tpl", "resources:\n  - registry.yaml\n")
	writeFile(t, repoRoot, "deploy/base/bootstrap-foundation/registry.yaml.tpl", "image: {{ image \"registry\" }}\n")
	writeFile(t, repoRoot, "deploy/base/bootstrap-builder-smoke/kustomization.yaml.tpl", "resources:\n  - kaniko-smoke.yaml\n")
	writeFile(t, repoRoot, "deploy/base/bootstrap-builder-smoke/kaniko-smoke.yaml.tpl", "image: {{ image \"kaniko-executor\" }}\n")
	writeFile(t, repoRoot, "deploy/base/web-public-foundation/kustomization.yaml.tpl", "resources:\n  - traefik.yaml\n  - cluster-issuer.yaml\n")
	writeFile(t, repoRoot, "deploy/base/web-public-foundation/traefik.yaml.tpl", "image: {{ image \"traefik\" }}\n")
	writeFile(t, repoRoot, "deploy/base/web-public-foundation/cluster-issuer.yaml.tpl", "email: {{ env \"KODEX_LETSENCRYPT_EMAIL\" }}\n")
	writeFile(t, repoRoot, "deploy/base/web-console-public/kustomization.yaml.tpl", "resources:\n  - oauth2-proxy.yaml\n  - certificate.yaml\n  - ingress.yaml\n")
	writeFile(t, repoRoot, "deploy/base/web-console-public/oauth2-proxy.yaml.tpl", "image: {{ image \"oauth2-proxy\" }}\n")
	writeFile(t, repoRoot, "deploy/base/web-console-public/certificate.yaml.tpl", "dns: {{ envOr \"KODEX_PRODUCTION_DOMAIN\" \"platform.kodex.works\" }}\n")
	writeFile(t, repoRoot, "deploy/base/web-console-public/ingress.yaml.tpl", "host: {{ envOr \"KODEX_PRODUCTION_DOMAIN\" \"platform.kodex.works\" }}\n")
	writeFile(t, repoRoot, "deploy/base/integration-gateway-public/kustomization.yaml.tpl", "resources:\n  - public-webhook-ingress.yaml\n")
	writeFile(t, repoRoot, "deploy/base/integration-gateway-public/public-webhook-ingress.yaml.tpl", "host: {{ envOr \"KODEX_PRODUCTION_DOMAIN\" \"platform.kodex.works\" }}\n")
	writeFile(t, repoRoot, "deploy/base/access-manager/kustomization.yaml.tpl", "resources:\n  - access-manager.yaml\n")
	writeFile(t, repoRoot, "deploy/base/access-manager/access-manager.yaml.tpl", "image: {{ image \"access-manager\" }}\n")
	writeFile(t, repoRoot, "deploy/base/access-manager/migrations.yaml.tpl", "image: {{ image \"access-manager-migrations\" }}\n")
	writeFile(t, repoRoot, "deploy/base/agent-manager/kustomization.yaml.tpl", "resources:\n  - agent-manager.yaml\n")
	writeFile(t, repoRoot, "deploy/base/agent-manager/agent-manager.yaml.tpl", "image: {{ image \"agent-manager\" }}\n"+strings.Join(requiredSelfDeployRenderedManifestFragments(), "\n")+"\n")
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

func assertRenderedTreeDoesNotContain(t *testing.T, root string, markers ...string) {
	t.Helper()
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, marker := range markers {
			if strings.Contains(string(content), marker) {
				t.Fatalf("rendered file %s leaked marker %q", path, marker)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("walk rendered tree: %v", err)
	}
}

func clearDeployPlanEnv(t *testing.T) {
	t.Helper()
	for _, item := range os.Environ() {
		name, _, ok := strings.Cut(item, "=")
		if ok && strings.HasPrefix(name, "KODEX_") {
			t.Setenv(name, "")
		}
	}
	for _, name := range []string{
		"KODEX_INTERNAL_REGISTRY_HOST",
		"KODEX_PRODUCTION_NAMESPACE",
		"KODEX_PRODUCTION_DOMAIN",
		"KODEX_PUBLIC_BASE_URL",
		"KODEX_GITHUB_OAUTH_CLIENT_ID",
		"KODEX_GITHUB_OAUTH_CLIENT_SECRET",
		"KODEX_POSTGRES_PASSWORD",
		"KODEX_ACCESS_MANAGER_DATABASE_DSN",
		"KODEX_ACCESS_MANAGER_GRPC_AUTH_TOKEN",
		"KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_PROJECT_ID",
		"KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_CONSUMER_ENABLED",
		"KODEX_AGENT_MANAGER_SELF_DEPLOY_GOVERNANCE_GATE_ENABLED",
		"KODEX_AGENT_MANAGER_SELF_DEPLOY_BUILD_DISPATCH_ENABLED",
	} {
		t.Setenv(name, "")
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) {
	return len(p), nil
}
