package app

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/codex"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	workspacepolicy "github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/workspace"
	"golang.org/x/sys/unix"
)

// TestRunnerOutboxPublicationContainer is inert without an explicit publication mode and requires runner UID 10001 before checking private-file denial or exact dual-UID modes and collected bytes/digests inside a disposable container; a direct inactive return is not container evidence.
// TestRunnerOutboxPublicationContainer неактивен без явного publication mode и требует runner UID 10001 до проверки отказа private-file либо точных dual-UID modes и collected bytes/digests в одноразовом контейнере; прямой неактивный выход не доказывает контейнерный сценарий.
func TestRunnerOutboxPublicationContainer(t *testing.T) {
	mode := os.Getenv("KODEX_OUTBOX_PUBLICATION_TEST")
	if mode == "" {
		return
	}
	if os.Geteuid() != 10001 {
		t.Fatal("collector fixture identity is invalid")
	}
	if mode == "MANAGED_WRITE" || mode == "MANAGED_COLLECT" || mode == "MANAGED_REJECT_FOREIGN" || mode == "MANAGED_GUARDS" || mode == "MANAGED_COMPLETION" {
		testManagedOutboxCollection(t, mode)
		return
	}
	if mode != "UNPUBLISHED" && mode != "COLLECT" {
		t.Fatal("collector fixture binding is invalid")
	}
	root := "/workspace"
	for _, name := range []string{"private.md", "atomic.md"} {
		path := filepath.Join(root, ".kodex/outbox", name)
		if mode == "UNPUBLISHED" {
			file, err := os.Open(path)
			if file != nil {
				file.Close()
			}
			if !errors.Is(err, syscall.EACCES) {
				t.Fatal("private provider artifact did not reproduce kernel EACCES for runner")
			}
		} else {
			var stat unix.Stat_t
			if unix.Stat(path, &stat) != nil || stat.Uid != 10002 || stat.Gid != 29000 || stat.Mode&0o7777 != 0o640 {
				t.Fatal("published artifact does not retain dual-UID ABI")
			}
		}
	}
	legacy := managedCollectionInput(root)
	legacy.WorkspaceAccess = "read-only"
	if runtimecontract.RuntimeWorkspaceToolsAvailable(legacy) {
		t.Fatal("read-only legacy fixture qualified for managed tools")
	}
	artifacts, err := collectArtifacts(legacy, "synthetic completion")
	if mode == "UNPUBLISHED" {
		if err == nil {
			t.Fatal("unpublished private artifacts were collected")
		}
		return
	}
	if err != nil || len(artifacts) != 3 {
		t.Fatal("published artifacts did not reach the canonical collector")
	}
	want := map[string]string{"result.md": "synthetic completion", "private.md": "private synthetic artifact", "atomic.md": "atomic synthetic artifact"}
	for _, artifact := range artifacts {
		body, ok := want[artifact.FileName]
		digest := sha256.Sum256([]byte(body))
		if !ok || string(artifact.Content) != body || artifact.SHA256 != hex.EncodeToString(digest[:]) {
			t.Fatal("published artifact bytes or digest changed")
		}
	}
}

// testManagedOutboxCollection uses the real runner UID to create/read a role-owned artifact,
// checks the canonical quota canary after writer join, and rejects a readable provider-owned inode.
// testManagedOutboxCollection создаёт и читает role-owned artifact с настоящим runner UID,
// проверяет canonical quota canary после writer join и отклоняет читаемый inode provider.
func testManagedOutboxCollection(t *testing.T, mode string) {
	t.Helper()
	root := "/workspace"
	path := filepath.Join(root, ".kodex/outbox/managed-report.md")
	body := []byte("synthetic managed report")
	if mode == "MANAGED_WRITE" {
		if os.WriteFile(path, body, 0o640) != nil {
			t.Fatal("create managed role-owned report")
		}
		return
	}
	input := managedCollectionInput(root)
	if !runtimecontract.RuntimeWorkspaceToolsAvailable(input) {
		t.Fatal("managed collection fixture is not qualified")
	}
	if mode == "MANAGED_COMPLETION" {
		testManagedCompletionPublication(t, input)
		return
	}
	if mode == "MANAGED_GUARDS" {
		testManagedOutboxGuards(t, input, path)
		return
	}
	if err := workspacepolicy.RunCanary(t.Context(), root, input.WorkspacePolicy); err != nil {
		t.Fatal("managed completion quota guard rejected fixture")
	}
	artifacts, err := collectArtifacts(input, "synthetic managed completion")
	if mode == "MANAGED_REJECT_FOREIGN" {
		if err == nil || len(artifacts) != 0 {
			t.Fatal("managed collector accepted a provider-owned file")
		}
		return
	}
	if err != nil || len(artifacts) != 2 {
		t.Fatal("managed role-owned report was not collected")
	}
	var metadata unix.Stat_t
	if unix.Stat(path, &metadata) != nil || metadata.Uid != 10001 || metadata.Gid != 29000 || metadata.Mode&0o7777 != 0o640 {
		t.Fatal("managed artifact metadata changed")
	}
	digest := sha256.Sum256(body)
	if artifacts[1].FileName != "managed-report.md" || string(artifacts[1].Content) != string(body) || artifacts[1].SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatal("managed artifact bytes or digest changed")
	}
}

// testManagedOutboxGuards checks real role-UID rejection of links and unsafe modes,
// then exceeds the published byte quota with a sparse file without allocating its payload.
// testManagedOutboxGuards проверяет отказ ссылок и небезопасных прав с настоящим role UID,
// затем превышает опубликованную квоту байтов sparse-файлом без выделения его payload.
func testManagedOutboxGuards(t *testing.T, input model.Input, report string) {
	t.Helper()
	link := filepath.Join(filepath.Dir(report), "link.md")
	for _, kind := range []string{"symlink", "hardlink", "mode", "private report"} {
		t.Run(kind, func(t *testing.T) {
			switch kind {
			case "symlink":
				if err := os.Symlink("managed-report.md", link); err != nil {
					t.Fatal("create synthetic symlink")
				}
				defer os.Remove(link)
			case "hardlink":
				if err := os.Link(report, link); err != nil {
					t.Fatal("create synthetic hardlink")
				}
				defer os.Remove(link)
			case "mode", "private report":
				mode := os.FileMode(0o660)
				if kind == "private report" {
					mode = 0o600
				}
				if err := os.Chmod(report, mode); err != nil {
					t.Fatal("change synthetic report mode")
				}
				defer os.Chmod(report, 0o640)
			}
			if artifacts, err := collectArtifacts(input, "synthetic completion"); err == nil || len(artifacts) != 0 {
				t.Fatal("managed collector accepted unsafe metadata")
			}
		})
	}
	quotaFile := filepath.Join(filepath.Dir(report), "quota.bin")
	file, err := os.OpenFile(quotaFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		t.Fatal("create synthetic quota fixture")
	}
	defer os.Remove(quotaFile)
	truncateErr := file.Truncate(input.WorkspacePolicy.MaximumWritableBytes + 1)
	closeErr := file.Close()
	if truncateErr != nil || closeErr != nil {
		t.Fatal("prepare synthetic quota fixture")
	}
	if err := workspacepolicy.RunCanary(t.Context(), input.WorkspaceRoot, input.WorkspacePolicy); workspacepolicy.DenialReason(err) != runtimecontract.RuntimeWorkspaceQuotaExceeded {
		t.Fatal("managed completion accepted quota overflow")
	}
}

// managedCollectionInput pins a synthetic managed turn without loading live credentials.
// managedCollectionInput закрепляет синтетический managed turn без загрузки живых credentials.
func managedCollectionInput(root string) model.Input {
	input := model.Input{Mode: runtimecontract.RunnerModeTurn, Provider: "opencode", AssistantScope: runtimecontract.AssistantScopeNone,
		WorkspaceRoot: root, WorkspaceAccess: "workspace-write", ProjectRef: "prj_fixture", LeaseRef: "lea_fixture", LeaseFence: "fixture-fence", LeaseGeneration: 1,
		InputDigest: strings.Repeat("a", 64), RuntimeRevisionDigest: strings.Repeat("b", 64), ExecutionBindingDigest: strings.Repeat("c", 64),
		RuntimeRevisionRef: "rrev_fixture", RuntimeRevisionVersion: 1, Attempt: 1,
		Capabilities: []string{runtimecontract.ArtifactCapability}, WorkspacePolicy: runtimecontract.RuntimeWorkspacePolicyV1(),
		ModelProvider: &runtimecontract.ModelProviderConfiguration{
			Schema: runtimecontract.ModelProviderConfigurationSchema, ManagedBy: "git", SourceRevision: strings.Repeat("a", 40),
			Engine: "opencode", EngineVersion: runtimecontract.OpenCodeVersion, ToolMode: "managed_mcp", ProviderID: "test-gateway",
			BaseURL: "https://gateway.example.invalid/v1", AddressCIDRs: []string{"192.0.2.40/32"},
			Models: []runtimecontract.ModelProviderModel{{ID: "model-a", ContextWindowTokens: 32768, MaximumOutputTokens: 1024}},
		}}
	input.EnvironmentPolicy = runtimecontract.DefaultRuntimeEnvironmentPolicy()
	input.EnvironmentPolicy.ManagedWorkspace = true
	return input
}

// testManagedCompletionPublication executes the real quota, provenance, collection and TLS callback
// chain. A synthetic result lacks a private native checkpoint seal and must reach that later guard,
// never fail artifact publication or weaken native-session validation for this filesystem fixture.
// testManagedCompletionPublication выполняет настоящую цепочку quota, provenance, collection и TLS callback.
// Синтетический результат без private native checkpoint seal должен дойти до этой последующей проверки,
// не падая на artifact publication и не ослабляя native-session validation ради filesystem fixture.
func testManagedCompletionPublication(t *testing.T, input model.Input) {
	t.Helper()
	calls := 0
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var payload runtimecontract.RunnerCompletionRequest
		if json.NewDecoder(r.Body).Decode(&payload) != nil || payload.Validate() != nil || payload.Success || payload.SafeErrorCode != "PROVIDER_RESPONSE_INVALID" || payload.ResultSummary != "i18n:RUNTIME_RESULT_INVALID" {
			t.Errorf("completion did not reach native checkpoint boundary: success=%t code=%s", payload.Success, payload.SafeErrorCode)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAnyClientCert}
	server.StartTLS()
	defer server.Close()
	client := completionTestClient(t, &input, server)
	result := codex.Result{Outcome: "SUCCEEDED", FinalMessage: "synthetic completion"}
	check := func(ctx context.Context) error {
		return workspacepolicy.RunCanary(ctx, input.WorkspaceRoot, input.WorkspacePolicy)
	}
	if err := completeExecutedTurn(t.Context(), input, client, result, check, nil); err != nil || calls != 1 {
		t.Fatal("managed completion callback was not delivered")
	}
	path := filepath.Join(input.WorkspaceRoot, ".kodex/outbox/workspace-write-result.json")
	var metadata unix.Stat_t
	if unix.Stat(path, &metadata) != nil || metadata.Uid != 10001 || metadata.Gid != 29000 || metadata.Mode&0o7777 != 0o600 {
		t.Fatal("completion provenance lost trusted private metadata")
	}
	artifacts, err := completionArtifacts(input, result.FinalMessage)
	if err != nil || len(artifacts) != 3 || artifacts[1].FileName != "workspace-write-result.json" {
		t.Fatal("completion did not collect private trusted provenance and managed report")
	}
	var provenance workspacepolicy.ResultProvenance
	provenanceArtifact := artifacts[1]
	digest := sha256.Sum256(provenanceArtifact.Content)
	if json.Unmarshal(provenanceArtifact.Content, &provenance) != nil || provenance.RuntimeRevisionDigest != input.RuntimeRevisionDigest || provenance.ExecutionBindingDigest != input.ExecutionBindingDigest || provenance.Attempt != input.Attempt || provenanceArtifact.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatal("completion provenance binding or hash changed")
	}
}
