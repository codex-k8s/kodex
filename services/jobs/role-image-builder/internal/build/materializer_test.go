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
		`{"vertexes":[{"name":"load metadata for registry/base@sha256:abc"}]}`:                                                  controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_BASE_PULL,
		`{"vertexes":[{"name":"RUN /bin/sh /run/mattercodex/install.sh"}]}`:                                                     controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_INSTALLATION,
		`{"vertexes":[{"name":"COPY --from=trusted-runtime /usr/local/bin/mattercodex-init /usr/local/bin/mattercodex-init"}]}`: controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_TRUSTED_RUNTIME_FINALIZATION,
		`{"vertexes":[{"name":"exporting to image"}]}`:                                                                          controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_STAGING_PUSH,
	} {
		if actual := phaseFromRawJSON([]byte(raw)); actual != expected {
			t.Fatalf("phaseFromRawJSON() = %s, want %s", actual, expected)
		}
	}
}

func TestBuildPhaseOrderIsMonotonic(t *testing.T) {
	t.Parallel()
	sequence := []controlplanev1.ImageBuildStage{
		controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_BASE_PULL,
		controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_SOLVING,
		controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_INSTALLATION,
		controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_TRUSTED_RUNTIME_FINALIZATION,
		controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_STAGING_PUSH,
	}
	for index := 1; index < len(sequence); index++ {
		if buildStageOrder(sequence[index]) <= buildStageOrder(sequence[index-1]) {
			t.Fatalf("valid build phase sequence regressed at %s", sequence[index])
		}
	}
	if buildStageOrder(controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_SOLVING) >=
		buildStageOrder(controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_TRUSTED_RUNTIME_FINALIZATION) {
		t.Fatal("real SOLVING regression is not rejected after finalization")
	}
}

func TestVersionedCredentialReferenceIsExact(t *testing.T) {
	t.Parallel()
	path, version, ok := parseVersionedCredentialReference("vault-versioned://builder/token/v7")
	if !ok || path != "builder/token" || version != 7 {
		t.Fatalf("canonical credential reference was rejected: %q %d %v", path, version, ok)
	}
	for _, reference := range []string{
		"k8s-immutable-secret://builder/token/v7", "vault-versioned://builder/token/latest",
		"vault-versioned://builder/../token/v7", "vault-versioned://builder/token/v0",
		"vault-versioned://builder/token/v7?version=8",
	} {
		if _, _, accepted := parseVersionedCredentialReference(reference); accepted {
			t.Fatalf("unsafe credential reference was accepted: %s", reference)
		}
	}
}

func TestRegistryAuthoritiesRemainPhysicallySeparated(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..", "..", "..", "deploy", "k8s", "base")
	buildkit, err := os.ReadFile(filepath.Join(root, "image-supply-chain", "buildkit.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	worker := string(buildkit)
	for _, required := range []string{"DOCKER_CONFIG=/var/run/secrets/mattercodex/buildkit/tls", "staging/readiness"} {
		if !strings.Contains(worker, required) {
			t.Fatalf("BuildKit auth path misses %s", required)
		}
	}
	daemon, err := os.ReadFile(filepath.Join(root, "image-supply-chain", "buildkitd.toml"))
	if err != nil || !strings.Contains(string(daemon), "pull-ca.pem") || !strings.Contains(string(daemon), "push-ca.pem") {
		t.Fatal("BuildKit daemon does not separate pull and push trust roots")
	}
	network, err := os.ReadFile(filepath.Join(root, "image-supply-chain", "networkpolicy.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	policy := string(network)
	pushStart := strings.Index(policy, "name: mattercodex-image-registry-push")
	pushEnd := strings.Index(policy[pushStart:], "\n---")
	if pushStart < 0 || pushEnd < 0 {
		t.Fatal("staging write NetworkPolicy is absent")
	}
	pushPolicy := policy[pushStart : pushStart+pushEnd]
	if strings.Contains(pushPolicy, "image-admission-phase") || !strings.Contains(pushPolicy, "mattercodex-buildkit") {
		t.Fatal("non-BuildKit phase received staging write network authority")
	}
	deployment, err := os.ReadFile(filepath.Join(root, "role-image-builder", "deployment.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(deployment), "mattercodex-image-registry-push.mattercodex-system.svc.cluster.local:5001") {
		t.Fatal("builder client received direct staging push destination")
	}
}
