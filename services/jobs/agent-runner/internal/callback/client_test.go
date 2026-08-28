package callback

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func TestNextWarmAcceptsTurnWithCompatibleRuntime(t *testing.T) {
	turn := validWarmTurnFixture()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer ticket" ||
			request.Header.Get("X-Kodex-Runtime-Revision") != "system-assistant-core-v1" ||
			request.Header.Get("X-Kodex-Runtime-Revision-Digest") != strings.Repeat("f", 64) {
			http.Error(writer, "invalid binding", http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(turn)
	}))
	defer server.Close()
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{http: server.Client(), base: base, token: "ticket"}
	warm := turn
	warm.Mode = runtimecontract.RunnerModeWarm
	warm.RunRef, warm.NodeRef, warm.TurnRef = "", "", ""
	warm.Attempt, warm.LeaseRef, warm.LeaseFence, warm.LeaseGeneration = 0, "", "", 0
	warm.Task = ""
	warm.RuntimeRevisionRef = "system-assistant-core-v1"
	warm.RuntimeRevisionDigest = strings.Repeat("f", 64)

	got, available, err := client.NextWarm(context.Background(), warm)
	if err != nil {
		t.Fatalf("NextWarm() error = %v", err)
	}
	if !available || got.RunRef != turn.RunRef {
		t.Fatalf("NextWarm() = available %v, run %q", available, got.RunRef)
	}
}

func TestPostReportsOnlySafeHTTPStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "sensitive internal diagnostic", http.StatusConflict)
	}))
	defer server.Close()
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{http: server.Client(), base: base, token: "ticket"}
	err = client.post(context.Background(), "/complete", map[string]string{"result": "bounded"})
	if err == nil || err.Error() != "runtime callback rejected request with status 409" {
		t.Fatalf("post() error = %v", err)
	}
	if strings.Contains(err.Error(), "sensitive") {
		t.Fatal("runtime callback response body escaped the provider boundary")
	}
}

func TestCompleteRejectsInvalidPayloadBeforeTransport(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	defer server.Close()
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{http: server.Client(), base: base, token: "ticket"}
	err = client.Complete(context.Background(), validWarmTurnFixture(), runtimecontract.RunnerCompletionRequest{})
	if err == nil || err.Error() != "validate runtime completion: runner completion is invalid" {
		t.Fatalf("Complete() error = %v", err)
	}
	if called {
		t.Fatal("invalid runtime completion reached the callback transport")
	}
}

func validWarmTurnFixture() runtimecontract.RunnerInput {
	imageDigest := "sha256:" + strings.Repeat("a", 64)
	return runtimecontract.RunnerInput{
		Schema: runtimecontract.RunnerInputSchemaV6, Mode: runtimecontract.RunnerModeTurn,
		WorkloadInstance: "runtime-controller-1", RunRef: "run_abcdefgh", NodeRef: "node_abcdefgh",
		SessionRef: "session_abcdefgh", TurnRef: "turn_abcdefgh", AgentRef: "agent_abcdefgh",
		Attempt: 1, LeaseRef: "lease_abcdefgh", LeaseFence: "fence-1", LeaseGeneration: 1,
		RuntimeRevisionRef: "revision_abcdefgh", RuntimeRevisionVersion: 1,
		RuntimeRevisionDigest: strings.Repeat("b", 64), ImageReference: "registry.example/roles@" + imageDigest,
		ImageManifestDigest: imageDigest, RoleRuntimeContractRevision: 1,
		RoleRuntimeContractSHA256: strings.Repeat("d", 64), SystemAssistant: true,
		Instructions: "Complete the bounded task.", Task: "Prepare the customer response.",
		Provider: "openai", Model: "codex", ProviderAccountRef: "pacc_abcdefgh",
		ProviderCredentialRef: "pcr_abcdefgh", ProviderCredentialRevision: 1,
		ProviderCredentialSHA256: strings.Repeat("e", 64),
		RuntimeConfigRef:         "rconf_abcdefgh", RuntimeConfigVersion: 1, RuntimeConfigDigest: strings.Repeat("1", 64),
		ProviderPolicyRef: "ppol_abcdefgh", ProviderPolicyVersion: 1, ProviderPolicyDigest: strings.Repeat("2", 64),
		ConfigOverlayRef: "cover_abcdefgh", ConfigOverlayVersion: 1,
		ConfigOverlayDigest:   "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		RuntimeEnvironmentRef: "renv_abcdefgh", RuntimeEnvironmentVersion: 1,
		RuntimeEnvironmentDigest: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		EnvironmentBindingRef:    "aenv_abcdefgh", EnvironmentBindingVersion: 1, EnvironmentBindingDigest: strings.Repeat("3", 64),
		CodexSandbox:        "read-only",
		CodexApprovalPolicy: "never", CallbackURL: "https://10.0.0.10:8444",
		CallbackTLS: runtimecontract.RuntimeTLSBinding{
			ServerName:      "runtime-controller-callback.kodex-system.svc.cluster.local",
			CAFile:          "/var/run/config/kodex/runtime/callback/ca.crt",
			CertificateFile: "/var/run/secrets/kodex/runtime/callback-client/tls.crt",
			PrivateKeyFile:  "/var/run/secrets/kodex/runtime/callback-client/tls.key",
		},
		ExecutionTicketFile:    "/var/run/secrets/kodex/runtime/ticket/token",
		ProviderAuthFile:       "/run/secrets/kodex/runtime/provider/auth.json",
		ProviderAuthSHA256File: "/run/secrets/kodex/runtime/provider/auth.sha256",
		WorkspaceRoot:          "/workspace", OutboxRoot: "/workspace/.kodex/outbox", CodexHome: "/workspace/.kodex/state/codex-home",
	}
}
