package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/credentialfs"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integrationfixture"
)

func TestNewUsesOnlyExactProviderEndpoints(t *testing.T) {
	t.Parallel()
	adapter, err := New(Config{
		CredentialDirectory: t.TempDir(),
		ProxyURL:            "http://egress-gateway.kodex-system.svc.cluster.local:8080",
		SyntheticBaseURL:    "http://integration-synthetic.kodex-system.svc.cluster.local:8080",
		Timeout:             10 * time.Second,
	})
	if err != nil || adapter == nil {
		t.Fatalf("New() error = %v", err)
	}
	for _, invalid := range []Config{
		{CredentialDirectory: t.TempDir(), ProxyURL: "http://other:8080", SyntheticBaseURL: "http://integration-synthetic.kodex-system.svc.cluster.local:8080", Timeout: 10 * time.Second},
		{CredentialDirectory: t.TempDir(), ProxyURL: "http://egress-gateway.kodex-system.svc.cluster.local:8080", SyntheticBaseURL: "http://forged.kodex-system.svc.cluster.local:8080", Timeout: 10 * time.Second},
	} {
		if _, err := New(invalid); err == nil {
			t.Fatal("New() accepted alternate provider endpoint")
		}
	}
}

func TestSyntheticHTTPJournalWriteIsIdempotentAndReadable(t *testing.T) {
	t.Parallel()
	fixture := integrationfixture.NewHandler(integrationfixture.NewStore())
	fixture.SetReady(true)
	server := httptest.NewServer(fixture)
	defer server.Close()

	adapter := testAdapter(t)
	adapter.syntheticBaseURL = mustParseURL(t, server.URL)
	adapter.syntheticClient = server.Client()
	write := invocationRequest(t, adapter.definitions["synthetic"], "synthetic.journal.write", map[string]any{"value": "first"}, nil)
	first, err := adapter.Execute(t.Context(), write)
	if err != nil {
		t.Fatal(err)
	}
	second, err := adapter.Execute(t.Context(), write)
	if err != nil {
		t.Fatal(err)
	}
	if first.Summary != second.Summary || first.Receipt != second.Receipt {
		t.Fatalf("duplicate synthetic effect was not deduplicated: %#v %#v", first, second)
	}
	read := invocationRequest(t, adapter.definitions["synthetic"], "synthetic.journal.read", map[string]any{}, nil)
	result, err := adapter.Execute(t.Context(), read)
	if err != nil || !strings.Contains(result.Summary, `"count":1`) {
		t.Fatalf("synthetic read = %#v, %v", result, err)
	}
}

func TestGitHubMetadataCreateRetryAndUpdateStayInsideRepository(t *testing.T) {
	t.Parallel()
	var mutex sync.Mutex
	var storedBody string
	createCalls, updateCalls := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer test-token" || !strings.HasPrefix(request.URL.Path, "/repos/acme/repo") {
			http.Error(writer, "forbidden", http.StatusForbidden)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		mutex.Lock()
		defer mutex.Unlock()
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/repos/acme/repo":
			_, _ = writer.Write([]byte(`{"id":42,"name":"repo","full_name":"acme/repo","private":true,"archived":false,"default_branch":"main","visibility":"private"}`))
		case request.Method == http.MethodGet && request.URL.Path == "/repos/acme/repo/issues":
			if storedBody == "" {
				_, _ = writer.Write([]byte(`[]`))
			} else {
				_ = json.NewEncoder(writer).Encode([]map[string]any{{"id": 77, "number": 3, "title": "created", "body": storedBody, "state": "open"}})
			}
		case request.Method == http.MethodPost && request.URL.Path == "/repos/acme/repo/issues":
			var input struct {
				Body string `json:"body"`
			}
			_ = json.NewDecoder(request.Body).Decode(&input)
			storedBody = input.Body
			createCalls++
			_, _ = writer.Write([]byte(`{"id":77,"number":3,"title":"created","state":"open"}`))
		case request.Method == http.MethodPatch && request.URL.Path == "/repos/acme/repo/issues/3":
			updateCalls++
			_, _ = writer.Write([]byte(`{"id":77,"number":3,"title":"updated","state":"closed"}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	adapter := testAdapter(t)
	adapter.githubHTTPClient = server.Client()
	adapter.githubBaseURL = mustParseURL(t, server.URL+"/")
	credential := testCredential(t, adapter, "test-token")
	metadata := invocationRequest(t, adapter.definitions["github"], "github.repository.metadata.read", map[string]any{}, credential)
	if result, err := adapter.Execute(t.Context(), metadata); err != nil || !strings.Contains(result.Summary, "acme/repo") {
		t.Fatalf("metadata result = %#v, %v", result, err)
	}
	create := invocationRequest(t, adapter.definitions["github"], "github.issue.create", map[string]any{"title": "created", "body": "body"}, credential)
	first, err := adapter.Execute(t.Context(), create)
	if err != nil {
		t.Fatal(err)
	}
	second, err := adapter.Execute(t.Context(), create)
	if err != nil {
		t.Fatal(err)
	}
	if first.Receipt.ProviderEffectRef != "github-issue:3" || second.Receipt.ProviderEffectRef != first.Receipt.ProviderEffectRef || createCalls != 1 {
		t.Fatalf("GitHub create retry was not reconciled: %#v %#v createCalls=%d", first, second, createCalls)
	}
	update := invocationRequest(t, adapter.definitions["github"], "github.issue.update", map[string]any{"issue_number": 3, "title": "updated", "state": "closed"}, credential)
	if _, err := adapter.Execute(t.Context(), update); err != nil || updateCalls != 1 {
		t.Fatalf("GitHub update failed: %v, calls=%d", err, updateCalls)
	}
}

func TestCredentialRevisionDigestMismatchFailsClosed(t *testing.T) {
	t.Parallel()
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "test-token")
	credential.ContentSHA256 = strings.Repeat("0", 64)
	if _, _, err := adapter.githubClient(t.Context(), credential); err == nil {
		t.Fatal("githubClient() accepted credential content digest mismatch")
	}
}

func TestCredentialReadWaitsForProjectedSecretKey(t *testing.T) {
	t.Parallel()
	adapter := testAdapter(t)
	root := t.TempDir()
	store, err := credentialfs.New(root)
	if err != nil {
		t.Fatal(err)
	}
	adapter.credentials = store
	value := []byte("projected-token")
	digest := sha256.Sum256(value)
	credential := &CredentialRevision{
		Ref: "icr_projected", Revision: 1,
		SecretRef: "kodex-system/kodex-integration-credentials#projected-token",
		SecretUID: "3f18ba8c-8829-4c7f-8350-b8ed65f80d41", SecretResourceVersion: "18",
		ContentSHA256: hex.EncodeToString(digest[:]),
	}
	writeResult := make(chan error, 1)
	go func() {
		time.Sleep(25 * time.Millisecond)
		writeResult <- os.WriteFile(filepath.Join(root, "projected-token"), value, 0o440)
	}()
	_, cleanup, err := adapter.githubClient(t.Context(), credential)
	if err != nil {
		t.Fatalf("githubClient() did not wait for projected credential: %v", err)
	}
	cleanup()
	if err := <-writeResult; err != nil {
		t.Fatalf("write projected credential: %v", err)
	}
}

func TestOutcomeExposesOnlySafeCode(t *testing.T) {
	t.Parallel()
	success, code := Outcome(errors.New("raw provider response"))
	if success || code != "INTEGRATION_UNAVAILABLE" {
		t.Fatalf("Outcome() = %v, %q", success, code)
	}
}

func TestEmailRetriesHealthAndUsesProviderNativeIdempotency(t *testing.T) {
	t.Parallel()
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "email-token")
	requests := 0
	adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if request.URL.Host != "email.example.test" || request.Header.Get("Authorization") != "Bearer email-token" {
			t.Fatalf("unexpected provider request: %s %q", request.URL, request.Header.Get("Authorization"))
		}
		status, body := http.StatusOK, `{"status":"ready"}`
		if request.Method == http.MethodGet && requests == 1 {
			status, body = http.StatusServiceUnavailable, `{"error":"temporarily unavailable"}`
		}
		if request.Method == http.MethodPost {
			if request.Header.Get("Idempotency-Key") == "" {
				t.Fatal("email send does not carry the effect key")
			}
			body = `{"message_id":"msg-1","status":"accepted","provider_debug":"must-not-leak"}`
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}

	health := invocationRequest(t, adapter.definitions["email"], "email.delivery.health.read", map[string]any{}, credential)
	if result, err := adapter.Execute(t.Context(), health); err != nil || result.Summary != `{"status":"ready"}` || requests != 2 {
		t.Fatalf("email health = %#v, %v, requests=%d", result, err, requests)
	}
	send := invocationRequest(t, adapter.definitions["email"], "email.message.send", map[string]any{
		"to": "recipient@example.test", "subject": "Hello", "body_text": "Body",
	}, credential)
	result, err := adapter.Execute(t.Context(), send)
	if err != nil || !strings.Contains(result.Summary, `"message_id":"msg-1"`) || strings.Contains(result.Summary, "provider_debug") {
		t.Fatalf("email send = %#v, %v", result, err)
	}
	if send.Risk != "SENSITIVE" || send.ApprovalPolicy != "HUMAN_EACH_EFFECT" {
		t.Fatalf("email send policy = %s/%s", send.Risk, send.ApprovalPolicy)
	}
}

func TestGitLabMetadataUsesExactProjectScope(t *testing.T) {
	t.Parallel()
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "gitlab-token")
	adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.Host != "gitlab.example.test" ||
			request.URL.EscapedPath() != "/api/v4/projects/group%2Fproject" {
			t.Fatalf("unexpected GitLab request: %s %s", request.Method, request.URL)
		}
		body := `{"id":7,"path_with_namespace":"group/project","default_branch":"main","visibility":"private","archived":false}`
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	invocation := invocationRequest(t, adapter.definitions["gitlab"], "gitlab.project.metadata.read", map[string]any{}, credential)
	result, err := adapter.Execute(t.Context(), invocation)
	if err != nil || !strings.Contains(result.Summary, `"path_with_namespace":"group/project"`) {
		t.Fatalf("GitLab metadata = %#v, %v", result, err)
	}
}

func testAdapter(t *testing.T) *Adapter {
	t.Helper()
	root := t.TempDir()
	store, err := credentialfs.New(root)
	if err != nil {
		t.Fatal(err)
	}
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	return &Adapter{
		credentials: store, definitions: definitions, timeout: 10 * time.Second,
		githubHTTPClient: &http.Client{Timeout: 10 * time.Second}, githubBaseURL: mustURL(githubAPIBaseURL),
		providerHTTPClient: &http.Client{Timeout: 10 * time.Second},
		syntheticClient:    &http.Client{Timeout: 10 * time.Second}, syntheticBaseURL: mustURL("http://" + syntheticServiceHost + ":8080"),
	}
}

func invocationRequest(t *testing.T, definition integrationpackage.Package, capabilityKey string, input map[string]any, credential *CredentialRevision) Request {
	t.Helper()
	capability, ok := definition.Capability(capabilityKey)
	if !ok {
		t.Fatal("capability is missing")
	}
	configuration := map[string]string{"journal": "main"}
	if definition.Metadata.Key == "github" {
		configuration = map[string]string{"owner": "acme", "repository": "repo"}
	}
	switch definition.Metadata.Key {
	case "gitlab":
		configuration = map[string]string{"base_url": "https://gitlab.example.test", "project_path": "group/project"}
	case "jira":
		configuration = map[string]string{"base_url": "https://jira.example.test", "auth_scheme": "BEARER", "project_key": "OPS", "issue_type": "Task"}
	case "confluence":
		configuration = map[string]string{"base_url": "https://confluence.example.test", "auth_scheme": "BEARER", "space_id": "42"}
	case "email":
		configuration = map[string]string{"base_url": "https://email.example.test", "from_address": "sender@example.test"}
	}
	scope, err := capability.ResourceScopeValues(configuration)
	if err != nil {
		t.Fatal(err)
	}
	encodedScope, _ := json.Marshal(scope)
	scopeDigest := sha256.Sum256(encodedScope)
	encodedInput, _ := json.Marshal(input)
	canonicalInput, err := capability.ValidateInput(encodedInput)
	if err != nil {
		t.Fatal(err)
	}
	inputDigest := sha256.Sum256(canonicalInput)
	configurationAny := make(map[string]any, len(configuration))
	for key, value := range configuration {
		configurationAny[key] = value
	}
	return Request{
		DefinitionKey: definition.Metadata.Key, DefinitionVersion: definition.Metadata.Version,
		DefinitionDigest: definition.Digest, ConnectionRef: "int_test", CapabilityKey: capability.Key,
		Operation: capability.Operation, Risk: capability.Risk, ApprovalPolicy: capability.ApprovalPolicy,
		ResourceKind: capability.ResourceScope.Kind, ResourceScope: scope,
		ResourceScopeDigest: hex.EncodeToString(scopeDigest[:]), EffectKey: "eff_0123456789abcdef0123456789abcdef",
		InputDigest: hex.EncodeToString(inputDigest[:]), Configuration: configurationAny, Input: input, Credential: credential,
	}
}

func testCredential(t *testing.T, adapter *Adapter, value string) *CredentialRevision {
	t.Helper()
	root := t.TempDir()
	store, err := credentialfs.New(root)
	if err != nil {
		t.Fatal(err)
	}
	adapter.credentials = store
	if err := os.WriteFile(filepath.Join(root, "github-token"), []byte(value), 0o440); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(value))
	return &CredentialRevision{
		Ref: "icr_test", Revision: 1,
		SecretRef: "kodex-system/kodex-integration-credentials#github-token",
		SecretUID: "3f18ba8c-8829-4c7f-8350-b8ed65f80d41", SecretResourceVersion: "17",
		ContentSHA256: hex.EncodeToString(digest[:]),
	}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
