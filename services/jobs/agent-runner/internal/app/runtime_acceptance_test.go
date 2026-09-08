package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/callback"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/codex"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/workspace"
)

// Это локальный рабочий runTurn с настоящими TLS callbacks и subprocess CRUD.
// Provider и startup dependencies заменены fixtures; live CODEX_SHELL этот тест
// не доказывает. Потерянный ACK повторяет только completion, а не сам turn.
func TestRuntimeTurnContextWorkspaceAndRetriedCompletion(t *testing.T) {
	for _, mode := range []string{"agent", "workflow", "assistant", "cold continuation", "provider resume", "stale notice"} {
		t.Run(mode, func(t *testing.T) {
			input := acceptanceTurnFixture(t, mode)
			input.WorkspaceRoot = workspaceProcessFixture(t)
			continuation := mode == "cold continuation" || mode == "provider resume" || mode == "stale notice"
			notice := continuationContextFixture(t, input)
			if continuation {
				input.SessionContext = []runtimecontract.RunnerSessionMessage{{Role: "USER", Content: "Prior question"}, {Role: "ASSISTANT", Content: "Prior answer"}, {Role: "USER", Content: notice}}
			}
			if mode == "stale notice" {
				input.TurnRef = "turn_replaced"
			}
			wantSuccess := mode != "stale notice"
			usage := runtimecontract.TokenUsage{InputTokens: 40, OutputTokens: 10, TotalTokens: 50}
			var mu sync.Mutex
			var receipt []byte
			progress, completions, executions := 0, 0, 0
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("c", 64) ||
					r.Header.Get("X-Kodex-Attempt") != "2" || r.Header.Get("X-Kodex-Runtime-Revision-Digest") != input.RuntimeRevisionDigest || r.TLS == nil || len(r.TLS.PeerCertificates) != 1 {
					t.Error("runtime callback lost authenticated execution binding")
				}
				switch r.URL.Path {
				case "/v1/executions/lease_fixture/progress":
					progress++
					w.WriteHeader(http.StatusNoContent)
				case "/v1/executions/lease_fixture/complete":
					raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
					var payload runtimecontract.RunnerCompletionRequest
					if err != nil || json.Unmarshal(raw, &payload) != nil || payload.Validate() != nil || payload.Success != wantSuccess || payload.Attempt != input.Attempt || payload.RuntimeRevisionDigest != input.RuntimeRevisionDigest {
						t.Error("runtime completion lost exact result binding")
					}
					if !wantSuccess {
						if payload.SafeErrorCode != "RUNTIME_INPUT_INVALID" || payload.Usage != (runtimecontract.TokenUsage{}) || len(payload.Artifacts) != 0 {
							t.Error("stale notice was executed or published")
						}
					} else {
						if payload.Usage != usage {
							t.Error("measured usage was lost")
						}
						if mode == "assistant" {
							if len(payload.Artifacts) != 0 {
								t.Error("assistant published a project artifact")
							}
						} else {
							if len(payload.Artifacts) != 3 {
								t.Error("workspace result was not delivered")
							}
							foundResult, foundProvenance := false, false
							for _, artifact := range payload.Artifacts {
								if artifact.FileName == "agent-result.txt" {
									foundResult = string(artifact.Content) == "create/read/atomic-replace/read/delete completed"
								}
								if artifact.FileName == "workspace-write-result.json" {
									var provenance workspace.ResultProvenance
									foundProvenance = json.Unmarshal(artifact.Content, &provenance) == nil && provenance.RuntimeRevisionRef == input.RuntimeRevisionRef && provenance.RuntimeRevisionVersion == input.RuntimeRevisionVersion && provenance.RuntimeRevisionDigest == input.RuntimeRevisionDigest && provenance.Attempt == input.Attempt && provenance.ExecutionBindingDigest == input.ExecutionBindingDigest
								}
							}
							if !foundResult || !foundProvenance {
								t.Error("agent CRUD or immutable provenance was lost")
							}
						}
					}
					completions++
					if receipt == nil {
						receipt = append([]byte(nil), raw...)
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					if !bytes.Equal(receipt, raw) {
						t.Error("completion retry changed the accepted receipt")
					}
					w.WriteHeader(http.StatusNoContent)
				default:
					t.Error("unexpected runtime callback")
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAnyClientCert}
			server.StartTLS()
			defer server.Close()
			client := completionTestClient(t, &input, server)
			proxy := &turnProxyFixture{t: t}
			ready, cleaned := false, false
			runtime := turnRuntime{
				prepare: func(ctx context.Context, _ model.Input, _ *callback.Client) (preparedTurn, string, error) {
					return preparedTurn{ctx: ctx, proxy: proxy, cancel: func() { cleaned = true }}, "", nil
				},
				execute: func(ctx context.Context, actual model.Input, prompt []byte, socket, token string) (codex.Result, error) {
					executions++
					if !ready || actual.RuntimeRevisionDigest != input.RuntimeRevisionDigest || socket != proxy.SocketPath() || token != proxy.LocalBearerToken() {
						t.Fatal("provider started outside its prepared execution")
					}
					if continuation {
						messages := promptSessionMessages(t, prompt)
						count := 3
						if mode == "provider resume" {
							count = 1
						}
						if len(messages) != count || messages[len(messages)-1].Content != notice || strings.Contains(string(prompt), "<runtime-revision-delta>") {
							t.Fatal("ordinary runTurn lost or duplicated continuation")
						}
					} else if string(prompt) != input.Task {
						t.Fatal("ordinary runTurn changed its initial task")
					}
					if mode != "assistant" {
						runWorkspaceProcess(t, input.WorkspaceRoot, "positive")
					}
					return codex.Result{Outcome: "SUCCEEDED", FinalMessage: "Synthetic runtime result", Usage: usage}, nil
				},
				checkWorkspace: func(ctx context.Context) error {
					return workspace.RunCanary(ctx, input.WorkspaceRoot, input.WorkspacePolicy)
				},
			}
			if err := runTurn(t.Context(), input, client, func() { ready = true }, runtime); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			wantExecutions := 1
			if !wantSuccess {
				wantExecutions = 0
			}
			if executions != wantExecutions || completions != 2 || progress != 1 || !cleaned || !proxy.closed {
				t.Fatal("runtime repeated execution or lost terminal delivery and cleanup")
			}
		})
	}
}

func acceptanceTurnFixture(t *testing.T, mode string) model.Input {
	t.Helper()
	kind := "AGENT"
	if mode == "workflow" {
		kind = "WORKFLOW_STAGE"
	}
	input := semanticRunnerFixture(kind)
	input.RuntimeRevisionRef, input.RuntimeRevisionVersion, input.RuntimeRevisionDigest = "rrev_current", 4, strings.Repeat("a", 64)
	input.SessionRef, input.TurnRef, input.Attempt, input.LeaseRef = "ses_current", "turn_current", 2, "lease_fixture"
	input.ExecutionBindingDigest = strings.Repeat("b", 64)
	input.WorkspacePolicy = runtimecontract.RuntimeWorkspacePolicyV1()
	input.SystemAssistant = mode == "assistant"
	if mode == "provider resume" {
		input.CodexSessionID = "previous-provider-thread"
	}
	if !input.SystemAssistant {
		input.Capabilities = []string{runtimecontract.ArtifactCapability}
		input.CodexSandbox = "workspace-write"
	}
	slots := []string{"PURPOSE", "INPUT", "CONSTRAINTS", "EFFECTIVE_CAPABILITIES", "FILES", "TOOLS", "INTEGRATIONS"}
	if kind == "WORKFLOW_STAGE" {
		slots = []string{"WORKFLOW", "STAGE", "PURPOSE", "EXPECTED_RESULT", "INPUT", "CONSTRAINTS", "EFFECTIVE_CAPABILITIES", "FILES", "TOOLS", "INTEGRATIONS"}
	}
	if kind == "SESSION_CONTINUATION" {
		slots = append(slots, "RUNTIME_CHANGES")
	}
	sections := []runtimecontract.PromptServiceSection{{Source: "USER_TEMPLATE", Content: "Approved agent instructions"}}
	for _, slot := range slots {
		content := ""
		if slot == "EFFECTIVE_CAPABILITIES" {
			content = strings.Join(input.Capabilities, "\n")
		}
		sections = append(sections, runtimecontract.PromptServiceSection{Source: "PLATFORM", Slot: slot, Content: content})
	}
	raw, err := json.Marshal(runtimecontract.PromptServiceEnvelope{Revision: runtimecontract.PromptServiceRevision, Locale: "en", Sections: sections})
	if err != nil {
		t.Fatal(err)
	}
	input.Instructions = string(raw)
	service, err := json.Marshal(struct {
		Revision, Locale, Kind string
		Slots                  []string
	}{runtimecontract.PromptServiceRevision, "en", kind, slots})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(service)
	input.PromptServiceTemplateDigest = hex.EncodeToString(digest[:])
	return input
}
