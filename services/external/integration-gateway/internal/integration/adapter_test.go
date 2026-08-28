package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	if _, _, err := adapter.githubClient(credential); err == nil {
		t.Fatal("githubClient() accepted credential content digest mismatch")
	}
}

func TestOutcomeExposesOnlySafeCode(t *testing.T) {
	t.Parallel()
	success, code := Outcome(errors.New("raw provider response"))
	if success || code != "INTEGRATION_UNAVAILABLE" {
		t.Fatalf("Outcome() = %v, %q", success, code)
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
		syntheticClient: &http.Client{Timeout: 10 * time.Second}, syntheticBaseURL: mustURL("http://" + syntheticServiceHost + ":8080"),
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
