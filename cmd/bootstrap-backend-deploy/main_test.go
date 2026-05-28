package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/stackinventory"
)

func TestResolveSecretValuesPreservesExistingAndGeneratesMissing(t *testing.T) {
	existing := secretSnapshot{
		"kodex-postgres": {
			"KODEX_POSTGRES_DB":       "kodex",
			"KODEX_POSTGRES_USER":     "kodex",
			"KODEX_POSTGRES_PASSWORD": "existing-password",
		},
		"kodex-platform-runtime": {
			"KODEX_ACCESS_MANAGER_GRPC_AUTH_TOKEN": "existing-access-token",
			"KODEX_PROJECT_CATALOG_DATABASE_DSN":   "postgres://kodex:old-password@postgres:5432/kodex_project_catalog?sslmode=disable",
		},
	}
	values, err := resolveSecretValues(existing, func(key string) (string, bool) {
		switch key {
		case "KODEX_PROVIDER_HUB_GRPC_AUTH_TOKEN":
			return "env-provider-token", true
		case "KODEX_ACCESS_MANAGER_DATABASE_NAME":
			return "custom_access", true
		default:
			return "", false
		}
	})
	if err != nil {
		t.Fatalf("resolve secrets: %v", err)
	}
	if got := values.Postgres["KODEX_POSTGRES_PASSWORD"]; got != "existing-password" {
		t.Fatalf("existing postgres password was not preserved: %q", got)
	}
	if got := values.Runtime["KODEX_ACCESS_MANAGER_GRPC_AUTH_TOKEN"]; got != "existing-access-token" {
		t.Fatalf("existing access token was not preserved: %q", got)
	}
	if got := values.Runtime["KODEX_PROVIDER_HUB_GRPC_AUTH_TOKEN"]; got != "env-provider-token" {
		t.Fatalf("env provider token was not used: %q", got)
	}
	if got := values.Runtime["KODEX_PACKAGE_HUB_ACCESS_MANAGER_GRPC_AUTH_TOKEN"]; got != "existing-access-token" {
		t.Fatalf("derived access token mismatch: %q", got)
	}
	if got := values.RenderEnv["KODEX_ACCESS_MANAGER_DATABASE_NAME"]; got != "custom_access" {
		t.Fatalf("database name override mismatch: %q", got)
	}
	if dsn := values.Runtime["KODEX_ACCESS_MANAGER_DATABASE_DSN"]; !strings.Contains(dsn, "custom_access") || !strings.Contains(dsn, "existing-password") {
		t.Fatalf("database DSN was not derived from preserved values: %q", dsn)
	}
	if dsn := values.Runtime["KODEX_PROJECT_CATALOG_DATABASE_DSN"]; strings.Contains(dsn, "old-password") || !strings.Contains(dsn, "existing-password") {
		t.Fatalf("stale database DSN was not normalized: %q", dsn)
	}
	if len(values.Generated) == 0 {
		t.Fatal("expected missing keys to be generated")
	}
}

func TestReadSecretUsesIgnoreNotFoundAndTreatsEmptyOutputAsAbsent(t *testing.T) {
	var gotArgs []string
	values, err := readSecretWithKubectl(context.Background(), "kodex-test", "kodex-postgres", func(_ context.Context, args ...string) ([]byte, error) {
		gotArgs = append([]string{}, args...)
		return []byte("\n"), nil
	})
	if err != nil {
		t.Fatalf("read secret: %v", err)
	}
	if len(values) != 0 {
		t.Fatalf("expected absent secret to return empty values, got %v", values)
	}
	wantArgs := []string{"-n", "kodex-test", "get", "secret", "kodex-postgres", "--ignore-not-found", "-o", "json"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("kubectl args mismatch:\ngot  %v\nwant %v", gotArgs, wantArgs)
	}
}

func TestReadSecretFailsClosedOnKubectlReadError(t *testing.T) {
	_, err := readSecretWithKubectl(context.Background(), "kodex-test", "kodex-platform-runtime", func(_ context.Context, args ...string) ([]byte, error) {
		return nil, errors.New("temporary api failure")
	})
	if err == nil {
		t.Fatal("expected kubectl read error to stop secret resolution")
	}
	if !strings.Contains(err.Error(), "read Kubernetes secret kodex-platform-runtime") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFirstRingImageBuildsRequireDockerfileAndTarget(t *testing.T) {
	stack, err := stackinventory.Parse([]byte(testStackInventory()))
	if err != nil {
		t.Fatalf("parse stack: %v", err)
	}
	builds, err := firstRingImageBuilds(stack)
	if err != nil {
		t.Fatalf("first ring builds: %v", err)
	}
	if len(builds) != len(firstRingImageNames) {
		t.Fatalf("unexpected build count: got %d want %d", len(builds), len(firstRingImageNames))
	}
	for _, build := range builds {
		if build.Dockerfile == "" || build.Target == "" || build.Destination == "" {
			t.Fatalf("incomplete build spec: %+v", build)
		}
	}
}

func TestKanikoBuildJobManifestDoesNotEmbedRuntimeSecrets(t *testing.T) {
	manifest := kanikoBuildJobManifest("kodex-test", "kodex-build-access-manager", "/repo", "kaniko:debug", "golang:alpine", imageBuild{
		Name:        "access-manager",
		ImageName:   "access-manager",
		Dockerfile:  "services/internal/access-manager/Dockerfile",
		Target:      "prod",
		Destination: "127.0.0.1:5000/kodex/access-manager:0.1.0",
	})
	for _, forbidden := range []string{"KODEX_POSTGRES_PASSWORD", "DATABASE_DSN", "GRPC_AUTH_TOKEN"} {
		if strings.Contains(manifest, forbidden) {
			t.Fatalf("build job manifest includes runtime secret marker %q: %s", forbidden, manifest)
		}
	}
	for _, expected := range []string{"--target=prod", "--build-arg=GOLANG_IMAGE=golang:alpine", "hostPath"} {
		if !strings.Contains(manifest, expected) {
			t.Fatalf("build job manifest missing %q: %s", expected, manifest)
		}
	}
}

func testStackInventory() string {
	versions := `spec:
  versions:
    platform-event-log:
      value: "0.1.0"
    access-manager:
      value: "0.1.0"
    project-catalog:
      value: "0.1.0"
    package-hub:
      value: "0.1.0"
    provider-hub:
      value: "0.1.0"
  images:
`
	images := ""
	for _, image := range firstRingImageNames {
		versionName := strings.TrimSuffix(image, "-migrations")
		if image == "platform-event-log-migrations" {
			versionName = "platform-event-log"
		}
		images += `    ` + image + `:
      repository: '127.0.0.1:5000/kodex/` + image + `'
      tagTemplate: '{{ version "` + versionName + `" }}'
      dockerfile: services/internal/` + versionName + `/Dockerfile
      target: prod
`
	}
	return versions + images
}
