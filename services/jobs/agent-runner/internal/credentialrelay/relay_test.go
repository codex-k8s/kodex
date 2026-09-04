package credentialrelay

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

func TestPeerUIDReadsUnixPeerAndAuthorizationAllowsOnlyProvider(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relay.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		connection, _ := listener.Accept()
		accepted <- connection
	}()
	client, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server := <-accepted
	defer server.Close()
	uid, err := peerUID(server)
	if err != nil {
		t.Fatalf("peerUID() error = %v", err)
	}
	if uid != uint32(os.Geteuid()) {
		t.Fatalf("peerUID() = %d, want %d", uid, os.Geteuid())
	}
	if !authorizedProviderUID(providerUID) || authorizedProviderUID(10001) || authorizedProviderUID(relayUID) {
		t.Fatal("provider relay accepted a non-provider peer UID")
	}
	if !authorizedRelayUID(relayUID) || authorizedRelayUID(providerUID) || authorizedRelayUID(10001) {
		t.Fatal("provider client accepted a non-relay peer UID")
	}
}

func TestWriteFullHandlesPartialWrites(t *testing.T) {
	want := bytes.Repeat([]byte("credential-relay-payload"), 1024)
	writer := &partialWriter{maximum: 7}
	if err := writeFull(writer, want); err != nil {
		t.Fatalf("writeFull() error = %v", err)
	}
	if !bytes.Equal(writer.buffer.Bytes(), want) || writer.calls < 2 {
		t.Fatalf("writeFull() calls = %d, bytes = %d", writer.calls, writer.buffer.Len())
	}
}

func TestWriteFullRejectsZeroLengthWrite(t *testing.T) {
	if err := writeFull(zeroWriter{}, []byte("payload")); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("writeFull() error = %v", err)
	}
}

func TestDecodeRequestRejectsPayloadOutsideBound(t *testing.T) {
	oversized := bytes.Repeat([]byte("x"), maximumRequestBytes+1)
	if _, err := decodeRequest(bytes.NewReader(oversized)); err == nil {
		t.Fatal("oversized provider credential relay request was accepted")
	}
}

func TestDecodeRequestRejectsUnknownFields(t *testing.T) {
	raw := []byte(`{"lease_ref":"lease_abcdefgh","refresh":{},"authentication":"forbidden"}`)
	if _, err := decodeRequest(bytes.NewReader(raw)); err == nil {
		t.Fatal("provider credential relay request with unknown fields was accepted")
	}
}

func TestValidateRequestRequiresExactExecutionBinding(t *testing.T) {
	input, payload := validRelayFixture()
	if _, err := validateRequest(input, payload); err != nil {
		t.Fatalf("validateRequest() error = %v", err)
	}
	for name, mutate := range map[string]func(*request){
		"lease":               func(value *request) { value.Input.LeaseRef = "lease_other123" },
		"fence":               func(value *request) { value.Input.LeaseFence = "fence-other" },
		"generation":          func(value *request) { value.Input.LeaseGeneration++ },
		"runtime revision":    func(value *request) { value.Refresh.RuntimeRevisionDigest = strings.Repeat("d", 64) },
		"credential revision": func(value *request) { value.Refresh.PreviousCredentialRevisionRef = "pcr_other123" },
		"credential digest":   func(value *request) { value.Refresh.PreviousContentSHA256 = strings.Repeat("e", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			changed := payload
			changed.Refresh.Authentication = append([]byte(nil), payload.Refresh.Authentication...)
			mutate(&changed)
			if _, err := validateRequest(input, changed); err == nil {
				t.Fatal("mismatched provider credential relay binding was accepted")
			}
		})
	}
}

func TestValidateRequestAcceptsOnlyCompatibleWarmTurn(t *testing.T) {
	turn, payload := validRelayFixture()
	turn.SystemAssistant = true
	payload.Input = turn
	warm := turn
	warm.Mode = runtimecontract.RunnerModeWarm
	warm.RunRef, warm.NodeRef, warm.TurnRef = "", "", ""
	warm.Attempt, warm.LeaseRef, warm.LeaseFence, warm.LeaseGeneration = 0, "", "", 0
	warm.InputDigest, warm.ExecutionBindingDigest, warm.MCPBindingDigest = "", "", ""
	warm.Task = ""
	warm.RuntimeRevisionRef = "system-assistant-core-v1"
	warm.RuntimeRevisionDigest = strings.Repeat("f", 64)
	got, err := validateRequest(warm, payload)
	if err != nil || got.LeaseRef != turn.LeaseRef {
		t.Fatalf("validateRequest(warm) = %#v, %v", got, err)
	}
	payload.Input.Model = "other-model"
	if _, err := validateRequest(warm, payload); err == nil {
		t.Fatal("warm relay accepted an incompatible turn")
	}
}

func TestRequestEncodingStaysInsideRelayBound(t *testing.T) {
	_, payload := validRelayFixture()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) >= maximumRequestBytes {
		t.Fatalf("valid request size = %d", len(raw))
	}
}

func validRelayFixture() (model.Input, request) {
	imageDigest := "sha256:" + strings.Repeat("a", 64)
	image := runtimecontract.RuntimeEnvironmentImage{ArtifactRef: "imgart_abcdefgh", RecipeRef: "imgrec_abcdefgh",
		RecipeGeneration: 1, Reference: "registry.example/roles@" + imageDigest, Digest: imageDigest}
	policy := runtimecontract.DefaultRuntimeEnvironmentPolicy()
	access, _ := runtimecontract.RuntimeKubernetesAccessForExecution(policy.KubernetesAccess,
		runtimecontract.RuntimeServiceAccountName("lease_abcdefgh"), runtimecontract.RuntimeTurnPodName("lease_abcdefgh"))
	environmentDigest, _ := runtimecontract.RuntimeEnvironmentDigest(nil, nil, image, nil, policy)
	input := model.Input{
		Schema: runtimecontract.RunnerInputSchemaV6, Mode: runtimecontract.RunnerModeTurn,
		OrganizationRef:  "org_abcdefgh",
		WorkloadInstance: "runtime-controller-1", RunRef: "run_abcdefgh", NodeRef: "node_abcdefgh",
		ProjectRef: "prj_abcdefgh", SessionRef: "session_abcdefgh", TurnRef: "turn_abcdefgh", AgentRef: "agent_abcdefgh",
		Attempt: 1, LeaseRef: "lease_abcdefgh", LeaseFence: "fence-1", LeaseGeneration: 1,
		InputDigest:        strings.Repeat("0", 64),
		RuntimeRevisionRef: "revision_abcdefgh", RuntimeRevisionVersion: 1,
		RuntimeRevisionDigest: strings.Repeat("a", 64), ImageReference: "registry.example/roles@" + imageDigest,
		ImageManifestDigest: imageDigest, EnvironmentImage: image, RoleRuntimeContractRevision: 1,
		RoleRuntimeContractSHA256: strings.Repeat("d", 64), RoleDefinitionRef: "roledef_abcdefgh",
		RuntimeProfileRef: "profile_abcdefgh", RuntimeProfileRevision: "profile-revision-1",
		InstructionRef: "instr_abcdefgh", InstructionDigest: strings.Repeat("5", 64),
		PromptTemplateRef: "prompt_abcdefgh", PromptTemplateDigest: strings.Repeat("6", 64),
		PromptMaterializationDigest: strings.Repeat("7", 64), Instructions: "Complete the bounded task.",
		Task: "Prepare the customer response.", Provider: "openai", Model: "codex",
		ProviderAccountRef: "pacc_abcdefgh", ProviderCredentialRef: "pcr_abcdefgh",
		ProviderCredentialRevision: 1, ProviderCredentialSHA256: strings.Repeat("b", 64),
		RuntimeConfigRef: "rconf_abcdefgh", RuntimeConfigVersion: 1, RuntimeConfigDigest: strings.Repeat("1", 64),
		ProviderPolicyRef: "ppol_abcdefgh", ProviderPolicyVersion: 1, ProviderPolicyDigest: strings.Repeat("2", 64),
		ConfigOverlayRef: "cover_abcdefgh", ConfigOverlayVersion: 1,
		ConfigOverlayDigest:   "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		RuntimeEnvironmentRef: "renv_abcdefgh", RuntimeEnvironmentVersion: 1,
		RuntimeEnvironmentDigest: environmentDigest, EnvironmentPolicy: policy, WorkspacePolicy: runtimecontract.RuntimeWorkspacePolicyV1(), EffectiveKubernetesAccess: access,
		EnvironmentBindingRef: "aenv_abcdefgh", EnvironmentBindingVersion: 1, EnvironmentBindingDigest: strings.Repeat("3", 64),
		CodexSandbox: "read-only", CodexApprovalPolicy: "never", CallbackURL: "https://10.0.0.10:8444",
		CallbackTLS: runtimecontract.RuntimeTLSBinding{ServerName: "runtime-controller-callback.kodex-system.svc.cluster.local",
			CAFile: "/var/run/config/kodex/runtime/callback/ca.crt", CertificateFile: "/var/run/secrets/kodex/runtime/callback-client/tls.crt",
			PrivateKeyFile: "/var/run/secrets/kodex/runtime/callback-client/tls.key"},
		ExecutionTicketFile: "/var/run/secrets/kodex/runtime/ticket/token",
		ProviderAuthFile:    "/run/secrets/kodex/runtime/provider/auth.json", ProviderAuthSHA256File: "/run/secrets/kodex/runtime/provider/auth.sha256",
		WorkspaceRoot: "/workspace", OutboxRoot: "/workspace/.kodex/outbox", CodexHome: "/workspace/.kodex/state/codex-home",
	}
	input.InputDigest, _ = runtimecontract.RuntimeBoundedInputDigest(input.BoundedInput)
	input.ExecutionBindingDigest, input.MCPBindingDigest, _ = runtimecontract.RuntimeExecutionBindingDigests(input)
	payload := request{Input: input, Refresh: runtimecontract.RunnerProviderCredentialRefreshRequest{
		RuntimeRevisionDigest:         input.RuntimeRevisionDigest,
		PreviousCredentialRevisionRef: input.ProviderCredentialRef,
		PreviousContentSHA256:         input.ProviderCredentialSHA256,
		Authentication:                []byte(`{"auth_mode":"chatgpt","tokens":{"refresh_token":"rotated"}}`),
	}}
	return input, payload
}

type partialWriter struct {
	buffer  bytes.Buffer
	maximum int
	calls   int
}

func (writer *partialWriter) Write(payload []byte) (int, error) {
	writer.calls++
	if len(payload) > writer.maximum {
		payload = payload[:writer.maximum]
	}
	return writer.buffer.Write(payload)
}

type zeroWriter struct{}

func (zeroWriter) Write([]byte) (int, error) { return 0, nil }
