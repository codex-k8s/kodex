package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func TestRunRejectsManifestBundleDigestMismatchBeforeKubernetesConfig(t *testing.T) {
	t.Parallel()

	bundlePath := writeTestDeploymentBundle(t, "registry.local/kodex/runtime-manager:0.1.0")

	err := run(context.Background(), []string{
		"apply",
		"--bundle-path", bundlePath,
		"--bundle-digest", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"--target-namespace", "kodex",
		"--service-key", "runtime-manager",
		"--expected-image", "runtime-manager|registry.local/kodex/runtime-manager:0.1.0|",
		"--rollout-target", "deployment/kodex/runtime-manager",
	})

	if err == nil || err.Error() != "deploy manifest bundle digest mismatch" {
		t.Fatalf("run() error = %v, want digest mismatch", err)
	}
}

func TestRunRejectsExpectedImageMismatchBeforeApply(t *testing.T) {
	t.Parallel()

	bundlePath := writeTestDeploymentBundle(t, "registry.local/kodex/runtime-manager:0.1.0")
	_, digest, err := readBundle(bundlePath)
	if err != nil {
		t.Fatalf("readBundle(): %v", err)
	}

	err = run(context.Background(), []string{
		"apply",
		"--bundle-path", bundlePath,
		"--bundle-digest", digest,
		"--target-namespace", "kodex",
		"--service-key", "runtime-manager",
		"--expected-image", "runtime-manager|registry.local/kodex/runtime-manager:0.2.0|",
		"--rollout-target", "deployment/kodex/runtime-manager",
	})

	if err == nil || err.Error() != "deploy expected image mismatch" {
		t.Fatalf("run() error = %v, want expected image mismatch", err)
	}
}

func TestApplyObjectRejectsClusterScopedManifest(t *testing.T) {
	t.Parallel()

	object := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]any{
			"name": "other",
		},
	}}
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	mapper := staticRESTMapper{mapping: &apiMeta.RESTMapping{
		Resource: schema.GroupVersionResource{Version: "v1", Resource: "namespaces"},
		Scope:    apiMeta.RESTScopeRoot,
	}}

	err := applyObject(context.Background(), client, mapper, "kodex", object)

	if err == nil || err.Error() != "deploy manifest cluster scoped object rejected" {
		t.Fatalf("applyObject() error = %v, want cluster-scoped rejection", err)
	}
}

func writeTestDeploymentBundle(t *testing.T, image string) string {
	t.Helper()
	root := t.TempDir()
	raw := strings.Join([]string{
		"apiVersion: apps/v1",
		"kind: Deployment",
		"metadata:",
		"  name: runtime-manager",
		"spec:",
		"  template:",
		"    spec:",
		"      containers:",
		"        - name: runtime-manager",
		"          image: " + image,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(root, "deployment.yaml"), []byte(raw), 0o640); err != nil {
		t.Fatalf("write deployment bundle: %v", err)
	}
	return root
}

type staticRESTMapper struct {
	mapping *apiMeta.RESTMapping
	err     error
}

func (m staticRESTMapper) RESTMapping(schema.GroupKind, ...string) (*apiMeta.RESTMapping, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.mapping == nil {
		return nil, errors.New("missing mapping")
	}
	return m.mapping, nil
}
