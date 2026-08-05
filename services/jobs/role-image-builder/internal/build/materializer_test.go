package build

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
)

func TestReadRegistryCredentialRequiresExactDestination(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, "config.json")
	auth := base64.StdEncoding.EncodeToString([]byte("reader:password"))
	if err := os.WriteFile(path, []byte(`{"auths":{"registry.example.test:5000":{"auth":"`+auth+`"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	username, password, err := readRegistryCredential(path, "registry.example.test:5000")
	if err != nil || username != "reader" || password != "password" {
		t.Fatalf("exact credential readback failed: %q %q %v", username, password, err)
	}
	if _, _, err := readRegistryCredential(path, "attacker.example.test"); err == nil {
		t.Fatal("credential was accepted for another destination")
	}
}

func TestMaterializerAcceptsOnlyExactImmutableRepository(t *testing.T) {
	t.Parallel()
	digest := "sha256:" + strings.Repeat("a", 64)
	materializer := &Materializer{repository: "registry.example.test:5000/mattercodex/inputs"}
	if !materializer.allowedRef("oci://registry.example.test:5000/mattercodex/inputs@" + digest) {
		t.Fatal("exact immutable input reference was rejected")
	}
	for _, reference := range []string{
		"oci://registry.example.test:5000/mattercodex/other@" + digest,
		"oci://registry.example.test:5000/mattercodex/inputs:latest",
		"oci://attacker.example.test/mattercodex/inputs@" + digest,
	} {
		if materializer.allowedRef(reference) {
			t.Fatalf("unsafe input reference was accepted: %s", reference)
		}
	}
}

func TestPhaseFromRawJSONUsesReachableBuildKitVertex(t *testing.T) {
	t.Parallel()
	for raw, expected := range map[string]controlplanev1.ImageBuildStage{
		`{"vertexes":[{"name":"load metadata for registry/base@sha256:abc"}]}`: controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_BASE_PULL,
		`{"vertexes":[{"name":"RUN /bin/sh /run/mattercodex/install.sh"}]}`:    controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_INSTALLATION,
		`{"vertexes":[{"name":"exporting to image"}]}`:                         controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_STAGING_PUSH,
	} {
		if actual := phaseFromRawJSON([]byte(raw)); actual != expected {
			t.Fatalf("phaseFromRawJSON() = %s, want %s", actual, expected)
		}
	}
}
