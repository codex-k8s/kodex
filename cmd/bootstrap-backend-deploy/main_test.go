package main

import (
	"context"
	"errors"
	"reflect"
	"sort"
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
	stack, err := stackinventory.Parse([]byte(testStackInventory(firstRingImageNames)))
	if err != nil {
		t.Fatalf("parse stack: %v", err)
	}
	builds, err := ringImageBuilds(stack, []backendRing{firstRing})
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

func TestSelectBackendRings(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{name: "default", value: "", want: []string{"first"}},
		{name: "first", value: "first", want: []string{"first"}},
		{name: "second", value: "second", want: []string{"second"}},
		{name: "staff", value: "staff", want: []string{"staff"}},
		{name: "all", value: "all", want: []string{"first", "second"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rings, err := selectBackendRings(tt.value)
			if err != nil {
				t.Fatalf("select rings: %v", err)
			}
			if got := ringNames(rings); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("rings mismatch: got %v want %v", got, tt.want)
			}
		})
	}
}

func TestSelectBackendRingsRejectsUnsupportedValue(t *testing.T) {
	_, err := selectBackendRings("web")
	if err == nil {
		t.Fatal("expected unsupported ring to fail")
	}
	if !strings.Contains(err.Error(), "expected first, second, staff, or all") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSecondRingImageBuildsExcludeStaffGateway(t *testing.T) {
	stack, err := stackinventory.Parse([]byte(testStackInventory(secondRingImageNames)))
	if err != nil {
		t.Fatalf("parse stack: %v", err)
	}
	builds, err := ringImageBuilds(stack, []backendRing{secondRing})
	if err != nil {
		t.Fatalf("second ring builds: %v", err)
	}
	if len(builds) != len(secondRingImageNames) {
		t.Fatalf("unexpected build count: got %d want %d", len(builds), len(secondRingImageNames))
	}
	for _, build := range builds {
		if build.ImageName == "staff-gateway" {
			t.Fatal("staff-gateway must not be part of the second backend ring")
		}
		if build.Dockerfile == "" || build.Target == "" || build.Destination == "" {
			t.Fatalf("incomplete build spec: %+v", build)
		}
	}
}

func TestAllRingImageBuildsDeduplicateSharedImages(t *testing.T) {
	imageNames := append([]string{}, firstRingImageNames...)
	imageNames = append(imageNames, secondRingImageNames...)
	stack, err := stackinventory.Parse([]byte(testStackInventory(imageNames)))
	if err != nil {
		t.Fatalf("parse stack: %v", err)
	}
	builds, err := ringImageBuilds(stack, []backendRing{firstRing, secondRing})
	if err != nil {
		t.Fatalf("all ring builds: %v", err)
	}
	seen := map[string]int{}
	for _, build := range builds {
		seen[build.ImageName]++
	}
	if seen["platform-event-log-migrations"] != 1 {
		t.Fatalf("shared platform-event-log migration image was not deduplicated: %v", seen["platform-event-log-migrations"])
	}
}

func TestStaffRingImageBuildsOnlyStaffGateway(t *testing.T) {
	stack, err := stackinventory.Parse([]byte(testStackInventory(staffRingImageNames)))
	if err != nil {
		t.Fatalf("parse stack: %v", err)
	}
	builds, err := ringImageBuilds(stack, []backendRing{staffRing})
	if err != nil {
		t.Fatalf("staff ring builds: %v", err)
	}
	if len(builds) != 1 {
		t.Fatalf("unexpected build count: got %d want 1", len(builds))
	}
	if builds[0].ImageName != "staff-gateway" {
		t.Fatalf("unexpected staff ring image: %s", builds[0].ImageName)
	}
}

func TestAllRingSelectionDoesNotIncludeStaffGateway(t *testing.T) {
	rings, err := selectBackendRings("all")
	if err != nil {
		t.Fatalf("select all rings: %v", err)
	}
	if got := ringNames(rings); !reflect.DeepEqual(got, []string{"first", "second"}) {
		t.Fatalf("all ring mismatch: got %v", got)
	}
	for _, imageName := range imageNamesForRings(rings) {
		if imageName == "staff-gateway" {
			t.Fatal("staff-gateway must be deployed through explicit staff ring, not all")
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

func testStackInventory(imageNames []string) string {
	versionNames := map[string]bool{"platform-event-log": true}
	for _, image := range imageNames {
		versionNames[strings.TrimSuffix(image, "-migrations")] = true
	}
	versions := "spec:\n  versions:\n"
	names := make([]string, 0, len(versionNames))
	for name := range versionNames {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		versions += `    ` + name + `:
      value: "0.1.0"
`
	}
	versions += "  images:\n"
	images := ""
	seenImages := map[string]bool{}
	for _, image := range imageNames {
		if seenImages[image] {
			continue
		}
		seenImages[image] = true
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
