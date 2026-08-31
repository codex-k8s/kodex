package build

import (
	"encoding/base64"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
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
	materializer := &Materializer{repository: "registry.example.test:5000/kodex/inputs"}
	if !materializer.allowedRef("oci://registry.example.test:5000/kodex/inputs@" + digest) {
		t.Fatal("exact immutable input reference was rejected")
	}
	for _, reference := range []string{
		"oci://registry.example.test:5000/kodex/other@" + digest,
		"oci://registry.example.test:5000/kodex/inputs:latest",
		"oci://attacker.example.test/kodex/inputs@" + digest,
	} {
		if materializer.allowedRef(reference) {
			t.Fatalf("unsafe input reference was accepted: %s", reference)
		}
	}
}

func TestPhaseFromRawJSONUsesReachableBuildKitVertex(t *testing.T) {
	t.Parallel()
	for raw, expected := range map[string]controlplanev1.ImageBuildStage{
		`{"vertexes":[{"name":"load metadata for registry/base@sha256:abc"}]}`:                                      controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_BASE_PULL,
		`{"vertexes":[{"name":"RUN /bin/sh /run/kodex/install.sh"}]}`:                                               controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_INSTALLATION,
		`{"vertexes":[{"name":"COPY --from=trusted-runtime /usr/local/bin/kodex-init /usr/local/bin/kodex-init"}]}`: controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_TRUSTED_RUNTIME_FINALIZATION,
		`{"vertexes":[{"name":"exporting to image"}]}`:                                                              controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_STAGING_PUSH,
	} {
		if actual := phaseFromRawJSON([]byte(raw)); actual != expected {
			t.Fatalf("phaseFromRawJSON() = %s, want %s", actual, expected)
		}
	}
}

func TestBuildPhaseTrackerAcceptsActualBuildKitOrdering(t *testing.T) {
	t.Parallel()
	tracker := newBuildPhaseTracker()
	rawEvents := []string{
		`{"vertexes":[{"name":"resolve dockerfile frontend"}]}`,
		`{"vertexes":[{"name":"load metadata for registry/base@sha256:abc"}]}`,
		`{"vertexes":[{"name":"RUN /bin/sh /run/kodex/install.sh"}]}`,
		`{"vertexes":[{"name":"COPY --from=trusted-runtime /usr/local/bin/kodex-init /usr/local/bin/kodex-init"}]}`,
		`{"vertexes":[{"name":"exporting to image"}]}`,
	}
	var actual []controlplanev1.ImageBuildStage
	for _, raw := range rawEvents {
		phases, err := tracker.observe(phaseFromRawJSON([]byte(raw)))
		if err != nil {
			t.Fatal(err)
		}
		for _, phase := range phases {
			actual = append(actual, phase.Stage)
		}
	}
	if !tracker.complete() || len(actual) != len(buildPhaseSequence) {
		t.Fatalf("actual BuildKit sequence was not completed: %v", actual)
	}
	for index := range buildPhaseSequence {
		if actual[index] != buildPhaseSequence[index] {
			t.Fatalf("phase %d = %s, want %s", index, actual[index], buildPhaseSequence[index])
		}
	}
}

func TestBuildProgressPipeReadsBuildctlStderr(t *testing.T) {
	t.Parallel()
	const progress = `{"vertexes":[{"name":"exporting to image"}]}`
	command := exec.Command("sh", "-c", `printf 'not-progress'; printf '%s\n' "$1" >&2`, "sh", progress)
	stream, err := buildProgressPipe(command)
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	actual, readErr := io.ReadAll(stream)
	waitErr := command.Wait()
	if readErr != nil || waitErr != nil {
		t.Fatalf("read BuildKit progress: read=%v wait=%v", readErr, waitErr)
	}
	if string(actual) != progress+"\n" {
		t.Fatalf("progress stream = %q, want exact stderr event", actual)
	}
}

func TestBuildPhaseTrackerRejectsRealRegression(t *testing.T) {
	t.Parallel()
	tracker := newBuildPhaseTracker()
	for _, stage := range buildPhaseSequence[:4] {
		if _, err := tracker.observe(stage); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tracker.observe(controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_STAGING_PUSH); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.observe(controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_BASE_PULL); err != nil {
		t.Fatalf("a repeated completed vertex must be idempotent: %v", err)
	}
	tracker = newBuildPhaseTracker()
	if _, err := tracker.observe(controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_BASE_PULL); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.observe(controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_INSTALLATION); err == nil {
		t.Fatal("a previously unseen phase regression was accepted")
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
	for _, required := range []string{"DOCKER_CONFIG=/var/run/secrets/kodex/buildkit/tls", "staging/readiness"} {
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
	pushStart := strings.Index(policy, "name: kodex-image-registry-push")
	pushEnd := strings.Index(policy[pushStart:], "\n---")
	if pushStart < 0 || pushEnd < 0 {
		t.Fatal("staging write NetworkPolicy is absent")
	}
	pushPolicy := policy[pushStart : pushStart+pushEnd]
	if strings.Contains(pushPolicy, "image-admission-phase") || !strings.Contains(pushPolicy, "kodex-buildkit") {
		t.Fatal("non-BuildKit phase received staging write network authority")
	}
	deployment, err := os.ReadFile(filepath.Join(root, "role-image-builder", "deployment.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(deployment), "kodex-image-registry-push.kodex-system.svc.cluster.local:5001") {
		t.Fatal("builder client received direct staging push destination")
	}
}
