package build

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

// pinnedInputTransport routes synthetic manifest/blob requests inside a test,
// avoiding sockets, TLS identities and actual registry credentials entirely.
// pinnedInputTransport направляет искусственные manifest/blob запросы внутри теста,
// полностью исключая sockets, TLS identities и настоящие registry credentials.
type pinnedInputTransport func(*http.Request) (*http.Response, error)

// RoundTrip delegates one request to the test's exact in-memory route handler;
// errors remain ordinary transport failures rather than real network activity.
// RoundTrip передаёт один запрос точному in-memory handler теста; ошибки остаются
// обычными transport failures, не настоящей сетевой активностью.
func (transport pinnedInputTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

// TestPinnedInputMaterializationKeepsExactDirectoriesAndPayload supplies hashed
// synthetic OCI manifest/blob responses, checks GET order and exact digest-named
// output for both package/tool directories, and requires no leftover partial file.
// It exercises local fetch/hash/publication code, not real TLS or registry policy.
// TestPinnedInputMaterializationKeepsExactDirectoriesAndPayload подаёт искусственные
// OCI manifest/blob с хешами, проверяет порядок GET и точный digest-named output
// в package/tool каталогах без оставшегося partial file. Проверяется локальный
// fetch/hash/publication, не настоящий TLS либо registry policy.
func TestPinnedInputMaterializationKeepsExactDirectoriesAndPayload(t *testing.T) {
	payload := []byte("pinned fixture payload")
	digest := sha256Hex(payload)
	manifest, err := json.Marshal(map[string]any{
		"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json",
		"layers": []any{map[string]any{"mediaType": "application/vnd.kodex.role-image-input.v1", "digest": "sha256:" + digest, "size": len(payload)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	manifestDigest := "sha256:" + sha256Hex(manifest)
	manifestPath, blobPath := "/v2/kodex/inputs/manifests/"+manifestDigest, "/v2/kodex/inputs/blobs/sha256:"+digest
	for _, kind := range []string{"packages", "tools"} {
		t.Run(kind, func(t *testing.T) {
			var paths []string
			transport := pinnedInputTransport(func(request *http.Request) (*http.Response, error) {
				paths = append(paths, request.URL.Path)
				if request.Method != http.MethodGet || request.URL.Host != "registry.example.test:5000" {
					return nil, errors.New("unexpected synthetic registry destination")
				}
				var body []byte
				switch request.URL.Path {
				case manifestPath:
					body = manifest
				case blobPath:
					body = payload
				default:
					return nil, errors.New("unexpected synthetic registry path")
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body))}, nil
			})
			materializer := &Materializer{repository: "registry.example.test:5000/kodex/inputs", host: "registry.example.test:5000", path: "kodex/inputs", client: &http.Client{Transport: transport}}
			directory := filepath.Join(t.TempDir(), kind)
			diagnostic, err := materializer.materializePinnedInput(t.Context(), "oci://"+materializer.repository+"@"+manifestDigest, digest, directory)
			if err != nil || diagnostic != "" {
				t.Fatal("exact synthetic pinned input was rejected")
			}
			actual, err := os.ReadFile(filepath.Join(directory, digest))
			if err != nil || !bytes.Equal(actual, payload) || len(paths) != 2 || paths[0] != manifestPath || paths[1] != blobPath {
				t.Fatal("pinned input changed route order or payload destination")
			}
			if _, err := os.Stat(filepath.Join(directory, digest+".part")); !os.IsNotExist(err) {
				t.Fatal("verified pinned input left partial output")
			}
		})
	}
}

// TestPinnedInputMaterializationKeepsClosedOrderAndFailureStages requires source
// rejection before SHA rejection with a nil HTTP client, then uses a synthetic 403
// transport for valid pins. All failures keep the original reason/stage/sentinel
// and must not create the destination directory or contact an actual registry.
// TestPinnedInputMaterializationKeepsClosedOrderAndFailureStages требует отказа
// source до SHA с nil HTTP-клиентом, затем использует synthetic 403 для верных pins.
// Сохраняются прежние reason/stage/sentinel; каталог не создаётся, настоящий
// registry не вызывается.
func TestPinnedInputMaterializationKeepsClosedOrderAndFailureStages(t *testing.T) {
	digest := strings.Repeat("a", 64)
	reference := "oci://registry.example.test:5000/kodex/inputs@sha256:" + strings.Repeat("b", 64)
	for _, scenario := range []struct {
		name, reference, digest, diagnostic, reason string
		client                                      *http.Client
	}{
		{"source before SHA", "invalid-source", "invalid-sha", "INPUT_FETCH_REJECTED", inputReasonContextRef, nil},
		{"SHA before HTTP", reference, "invalid-sha", "INPUT_FETCH_REJECTED", inputReasonSHASchema, nil},
		{"HTTP rejection", reference, digest, "INPUT_DIGEST_MISMATCH", "", &http.Client{Transport: rejectedInputTransport{}}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "payloads")
			calls, expectedCalls := 0, 0
			client := scenario.client
			if client != nil {
				expectedCalls = 1
				client = &http.Client{Transport: pinnedInputTransport(func(request *http.Request) (*http.Response, error) {
					calls++
					return scenario.client.Transport.RoundTrip(request)
				})}
			}
			materializer := &Materializer{repository: "registry.example.test:5000/kodex/inputs", host: "registry.example.test:5000", path: "kodex/inputs", client: client}
			diagnostic, err := materializer.materializePinnedInput(t.Context(), scenario.reference, scenario.digest, directory)
			if diagnostic != scenario.diagnostic || InputRejectionReason(err) != scenario.reason || !errors.Is(err, ErrMaterialization) || calls != expectedCalls {
				t.Fatal("pinned input helper changed rejection order or diagnostic")
			}
			if _, err := os.Stat(directory); !os.IsNotExist(err) {
				t.Fatal("rejected pinned input created destination")
			}
		})
	}
}

// TestReadRegistryCredentialRequiresExactDestination loads fabricated Basic auth
// from a temporary Docker config, accepts only its exact registry key and rejects
// reuse for a different destination without reading any real credential file.
// TestReadRegistryCredentialRequiresExactDestination читает вымышленные Basic auth
// из временного Docker config, принимает точный registry key и отклоняет другой
// destination без чтения настоящих реквизитов.
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

// TestMaterializerAcceptsOnlyExactImmutableRepository accepts a digest-pinned input
// reference for the configured repository and rejects foreign paths/hosts or tags;
// this lexical predicate test does not authenticate a registry server.
// TestMaterializerAcceptsOnlyExactImmutableRepository принимает digest-pinned ref
// настроенного repository и отклоняет чужие path/host либо tags; лексическая
// проверка не аутентифицирует настоящий registry server.
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

// TestPhaseFromRawJSONUsesReachableBuildKitVertex maps representative synthetic
// BuildKit vertex names to base/install/runtime-finalization/push stages, without
// executing those operations or claiming the sample events are a complete trace.
// TestPhaseFromRawJSONUsesReachableBuildKitVertex сопоставляет искусственные
// BuildKit vertex names с base/install/runtime-finalization/push без исполнения
// операций и без объявления примеров полным настоящим trace.
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

// TestBuildPhaseTrackerAcceptsActualBuildKitOrdering feeds the fixture vertex
// sequence into the tracker and checks the complete expected emitted stage order;
// it verifies normalization/state tracking, not a live BuildKit execution.
// TestBuildPhaseTrackerAcceptsActualBuildKitOrdering подаёт fixture vertex sequence
// в tracker и проверяет полный ожидаемый порядок stages; это нормализация/состояние,
// не настоящее исполнение BuildKit.
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

// TestBuildProgressPipeReadsBuildctlStderr runs a local printf-only shell fixture
// that writes different stdout/stderr and requires exact stderr JSON bytes from
// the progress pipe. No buildctl binary, registry or credentials are used.
// TestBuildProgressPipeReadsBuildctlStderr запускает локальный printf-only shell
// с разными stdout/stderr и требует точные stderr JSON-байты progress pipe.
// Buildctl, registry и реквизиты не используются.
func TestBuildProgressPipeReadsBuildctlStderr(t *testing.T) {
	t.Parallel()
	// progress is a synthetic push vertex used solely to distinguish stderr bytes.
	// progress — искусственный push vertex только для различения байтов stderr.
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

// TestBuildPhaseTrackerRejectsRealRegression accepts repeated completed vertices
// after a complete sequence but rejects an unseen earlier phase after later work;
// these synthetic events exercise the distinction between replay and regression.
// TestBuildPhaseTrackerRejectsRealRegression принимает повтор completed vertices
// после полной последовательности, но отклоняет новую раннюю фазу после поздней;
// искусственные события различают replay и regression.
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

// TestRegistryAuthoritiesRemainPhysicallySeparated inspects repository manifests
// for distinct pull/push trust roots, BuildKit-only staging network scope and no
// direct builder push destination. It is source inspection, not an applied-cluster
// reachability, TLS-handshake or credential-boundary test.
// TestRegistryAuthoritiesRemainPhysicallySeparated проверяет файлы manifests:
// разные pull/push trust roots, BuildKit-only staging scope и отсутствие прямого
// builder push destination. Это чтение исходников, не проверка развёрнутой сети,
// TLS handshake либо credential boundary кластера.
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
