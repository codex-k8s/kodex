package app

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/callback"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/codex"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

func TestExecutedTurnFailurePreservesUsageInRetriedCallback(t *testing.T) {
	usage := runtimecontract.TokenUsage{TotalTokens: 150, InputTokens: 100, CachedInputTokens: 30, CacheWriteInputTokens: 10, OutputTokens: 50, ReasoningOutputTokens: 20, ModelContextWindow: 32768}
	for _, mode := range []string{"workspace", "cancelled workspace", "empty message", "oversized message", "invalid utf8", "publish result", "collect artifacts", "completion archive", "provider failure", "provider outbox failure", "success", "before execution"} {
		t.Run(mode, func(t *testing.T) {
			input := model.Input{RuntimeRevisionRef: "rrev_fixture", RuntimeRevisionVersion: 1, RuntimeRevisionDigest: strings.Repeat("a", 64), Attempt: 3, LeaseRef: "lease_fixture", ExecutionBindingDigest: strings.Repeat("b", 64), WorkspaceRoot: t.TempDir(), WorkspacePolicy: runtimecontract.RuntimeWorkspacePolicyV1()}
			result := codex.Result{Outcome: "SUCCEEDED", FinalMessage: "synthetic result", Usage: usage,
				SessionID:           "00000000-0000-4000-8000-000000000001",
				ArchiveRelativePath: ".kodex/state/codex-home/sessions/2026/10/07/rollout-2026-10-07T00-00-00-00000000-0000-4000-8000-000000000001.jsonl",
				ArchiveSHA256:       strings.Repeat("d", 64), ArchiveSizeBytes: 26276}
			check := func(context.Context) error { return nil }
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			wantCode := "PROVIDER_RESPONSE_INVALID"
			switch mode {
			case "workspace", "cancelled workspace":
				check = func(context.Context) error { return errors.New("synthetic quota denial") }
				wantCode = "RUNTIME_INPUT_INVALID"
				if mode == "cancelled workspace" {
					cancel()
				}
			case "empty message":
				result.FinalMessage = " "
			case "oversized message":
				result.FinalMessage = strings.Repeat("x", 64<<10+1)
			case "invalid utf8":
				result.FinalMessage = string([]byte{0xff})
			case "publish result":
				input.CodexSandbox = "workspace-write"
				input.Capabilities = []string{runtimecontract.ArtifactCapability}
				wantCode = "RUNTIME_INPUT_INVALID"
			case "collect artifacts":
				input.Capabilities = []string{runtimecontract.ArtifactCapability}
				wantCode = "RUNTIME_ARTIFACT_INVALID"
			case "completion archive":
				result.SessionID = "incomplete-archive-binding"
			case "provider failure":
				result.Outcome, result.FailureCode = "FAILED", "usage_limit_exceeded"
				wantCode = "PROVIDER_RATE_LIMITED"
			case "provider outbox failure":
				result.Outcome, result.FailureCode = "FAILED", "RUNTIME_ARTIFACT_INVALID"
				wantCode = "RUNTIME_ARTIFACT_INVALID"
			case "success":
				wantCode = ""
			case "before execution":
				wantCode = "RUNTIME_INPUT_INVALID"
			}
			wantUsage := usage
			wantArchive := mode != "before execution" && mode != "completion archive"
			if mode == "before execution" {
				wantUsage = runtimecontract.TokenUsage{}
			}
			var mu sync.Mutex
			var receipt []byte
			attempts := 0
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
				if err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if r.Method != http.MethodPost || r.URL.Path != "/v1/executions/lease_fixture/complete" || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("c", 64) || r.Header.Get("X-Kodex-Attempt") != "3" || r.Header.Get("X-Kodex-Runtime-Revision-Digest") != input.RuntimeRevisionDigest || r.TLS == nil || len(r.TLS.PeerCertificates) != 1 {
					t.Error("completion lost authenticated execution binding")
				}
				var payload runtimecontract.RunnerCompletionRequest
				if json.Unmarshal(raw, &payload) != nil || payload.Validate() != nil || payload.Usage != wantUsage || payload.SafeErrorCode != wantCode || payload.Success != (mode == "success") || payload.RuntimeRevisionDigest != input.RuntimeRevisionDigest || payload.Attempt != input.Attempt || len(payload.Artifacts) != 0 {
					t.Error("completion lost measured usage or terminal provenance")
				}
				if wantArchive {
					if payload.CodexSessionID != result.SessionID || payload.ArchiveRelativePath != result.ArchiveRelativePath || payload.ArchiveSHA256 != result.ArchiveSHA256 || payload.ArchiveSizeBytes != result.ArchiveSizeBytes {
						t.Error("completion lost confirmed archive binding")
					}
				} else if payload.CodexSessionID != "" || payload.ArchiveRelativePath != "" || payload.ArchiveSHA256 != "" || payload.ArchiveSizeBytes != 0 {
					t.Error("completion published an unconfirmed archive binding")
				}
				if (mode == "provider outbox failure" || mode == "collect artifacts") && payload.ResultSummary != "i18n:RUNTIME_ARTIFACT_INVALID" {
					t.Error("artifact completion lost its closed failure summary")
				}
				mu.Lock()
				defer mu.Unlock()
				attempts++
				if receipt == nil {
					receipt = append([]byte(nil), raw...)
					// Receipt уже принят; потеря ACK не должна менять расход при повторе.
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				if !bytes.Equal(receipt, raw) {
					t.Error("retried completion changed committed receipt")
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAnyClientCert}
			server.StartTLS()
			defer server.Close()
			client := completionTestClient(t, &input, server)
			var err error
			if mode == "before execution" {
				err = completeFailure(ctx, input, client, "RUNTIME_INPUT_INVALID")
			} else {
				err = completeExecutedTurn(ctx, input, client, result, check)
			}
			if err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if attempts != 2 || len(receipt) == 0 {
				t.Fatal("completion retry did not preserve accepted receipt")
			}
		})
	}
}

func completionTestClient(t *testing.T, input *model.Input, server *httptest.Server) *callback.Client {
	t.Helper()
	directory := t.TempDir()
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	privateKey, err := x509.MarshalPKCS8PrivateKey(server.TLS.Certificates[0].PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string][]byte{"ca.pem": certificate, "client.pem": certificate, "client.key": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKey}), "ticket": []byte(strings.Repeat("c", 64))} {
		if err := os.WriteFile(filepath.Join(directory, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	input.CallbackURL = server.URL
	input.CallbackTLS = model.TLSBinding{ServerName: "127.0.0.1", CAFile: filepath.Join(directory, "ca.pem"), CertificateFile: filepath.Join(directory, "client.pem"), PrivateKeyFile: filepath.Join(directory, "client.key")}
	input.ExecutionTicketFile = filepath.Join(directory, "ticket")
	client, err := callback.New(*input)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client
}

type turnProxyFixture struct {
	t      *testing.T
	closed bool
}

func (*turnProxyFixture) SocketPath() string       { return "synthetic-mcp-socket" }
func (*turnProxyFixture) LocalBearerToken() string { return "synthetic-mcp-token" }
func (proxy *turnProxyFixture) Close(ctx context.Context) error {
	deadline, bounded := ctx.Deadline()
	if ctx.Err() != nil || !bounded || time.Until(deadline) > 5*time.Second {
		proxy.t.Error("proxy cleanup lost its independent bounded context")
	}
	proxy.closed = true
	return nil
}

func TestRunTurnDeliversMeasuredUsageAfterBrokerAndTimelineFailures(t *testing.T) {
	for _, mode := range []string{"timeline", "cancelled timeline", "partial broker", "cancelled broker", "verified timeline", "verified broker", "success", "preparation", "provider before effect"} {
		t.Run(mode, func(t *testing.T) {
			usage := runtimecontract.TokenUsage{TotalTokens: 70, InputTokens: 60, CachedInputTokens: 20, OutputTokens: 10, ReasoningOutputTokens: 3}
			input := model.Input{Mode: runtimecontract.RunnerModeTurn, Task: "synthetic task", RuntimeRevisionDigest: strings.Repeat("a", 64), Attempt: 3, LeaseRef: "lease_fixture", ExecutionBindingDigest: strings.Repeat("b", 64)}
			result := codex.Result{Outcome: "SUCCEEDED", FinalMessage: "synthetic result", Usage: usage,
				ToolCalls: []runtimecontract.NativeToolCall{{CallID: "call-one", Revision: 2, Kind: runtimecontract.NativeToolKindSleep, State: runtimecontract.NativeToolStateSucceeded,
					SafeResult: runtimecontract.NativeToolResultCompleted, DurationMS: 25, SafeParameters: map[string]any{"requested_duration_ms": int64(25)}}},
				SessionID:           "00000000-0000-4000-8000-000000000001",
				ArchiveRelativePath: ".kodex/state/codex-home/sessions/2026/10/07/rollout-2026-10-07T00-00-00-00000000-0000-4000-8000-000000000001.jsonl",
				ArchiveSHA256:       strings.Repeat("d", 64), ArchiveSizeBytes: 39301}
			wantCode := "PROVIDER_UNAVAILABLE"
			wantUsage := usage
			if strings.Contains(mode, "timeline") {
				wantCode = "RUNTIME_UNAVAILABLE"
			}
			if mode == "success" {
				wantCode = ""
			}
			if mode == "preparation" || mode == "provider before effect" {
				wantUsage = runtimecontract.TokenUsage{}
			}
			if mode == "preparation" {
				wantCode = "RUNTIME_INPUT_INVALID"
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var mu sync.Mutex
			var receipt []byte
			progress, timeline, completions := 0, 0, 0
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("c", 64) || r.Header.Get("X-Kodex-Attempt") != "3" ||
					r.Header.Get("X-Kodex-Runtime-Revision-Digest") != input.RuntimeRevisionDigest || r.TLS == nil || len(r.TLS.PeerCertificates) != 1 {
					t.Error("turn callback lost exact authenticated binding")
				}
				switch r.URL.Path {
				case "/v1/executions/lease_fixture/progress":
					progress++
					w.WriteHeader(http.StatusNoContent)
				case "/v1/executions/lease_fixture/native-tool-call":
					timeline++
					if mode == "timeline" || mode == "verified timeline" {
						w.WriteHeader(http.StatusForbidden)
						return
					}
					w.WriteHeader(http.StatusNoContent)
				case "/v1/executions/lease_fixture/complete":
					raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
					var payload runtimecontract.RunnerCompletionRequest
					if err != nil || json.Unmarshal(raw, &payload) != nil || payload.Validate() != nil || payload.Usage != wantUsage ||
						payload.Success != (mode == "success") || payload.SafeErrorCode != wantCode || payload.Attempt != input.Attempt || payload.RuntimeRevisionDigest != input.RuntimeRevisionDigest || len(payload.Artifacts) != 0 {
						t.Error("turn completion lost measured usage or terminal outcome")
					}
					if mode == "success" || strings.HasPrefix(mode, "verified") {
						if payload.CodexSessionID != result.SessionID || payload.ArchiveRelativePath != result.ArchiveRelativePath || payload.ArchiveSHA256 != result.ArchiveSHA256 || payload.ArchiveSizeBytes != result.ArchiveSizeBytes {
							t.Error("successful execution lost confirmed archive binding")
						}
					} else if payload.CodexSessionID != "" || payload.ArchiveRelativePath != "" || payload.ArchiveSHA256 != "" || payload.ArchiveSizeBytes != 0 {
						t.Error("failed execution published an unconfirmed archive binding")
					}
					completions++
					if completions == 1 {
						receipt = append([]byte(nil), raw...)
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					if !bytes.Equal(receipt, raw) {
						t.Error("lost ACK changed turn receipt")
					}
					w.WriteHeader(http.StatusNoContent)
				default:
					t.Error("unexpected callback route")
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAnyClientCert}
			server.StartTLS()
			defer server.Close()
			client := completionTestClient(t, &input, server)
			if strings.HasPrefix(mode, "verified") {
				input.WorkspaceRoot = t.TempDir()
				input.CodexHome = filepath.Join(input.WorkspaceRoot, ".kodex/state/codex-home")
				input.InputDigest, input.SessionRef, input.TurnRef = strings.Repeat("e", 64), "ses_fixture", "turn_fixture"
				path := filepath.Join(input.WorkspaceRoot, filepath.FromSlash(result.ArchiveRelativePath))
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0o640)
				if err != nil {
					t.Fatal(err)
				}
				_, writeErr := file.WriteString("{\"type\":\"session_meta\"}\n{\"type\":\"event_msg\"}\n")
				syncErr, closeErr := file.Sync(), file.Close()
				if writeErr != nil || syncErr != nil || closeErr != nil {
					t.Fatal("fixture source was not durably closed")
				}
				captured, err := codex.CaptureStoppedRollout(input, result.SessionID, path)
				if err != nil {
					t.Fatal("actual source capture failed")
				}
				captured.Outcome, captured.FinalMessage, captured.Usage, captured.ToolCalls = result.Outcome, result.FinalMessage, result.Usage, result.ToolCalls
				result = captured
			}
			proxy := &turnProxyFixture{t: t}
			ready, executions, cleaned := false, 0, false
			runtime := turnRuntime{
				prepare: func(ctx context.Context, _ model.Input, _ *callback.Client) (preparedTurn, string, error) {
					if mode == "preparation" {
						return preparedTurn{}, "RUNTIME_INPUT_INVALID", errors.New("synthetic preparation failure")
					}
					return preparedTurn{ctx: ctx, proxy: proxy, cancel: func() { cleaned = true }}, "", nil
				},
				execute: func(_ context.Context, _ model.Input, prompt []byte, socket, token string, onActivity func(runtimecontract.RuntimeActivity) error) (codex.Result, error) {
					executions++
					mu.Lock()
					if !ready || progress != 1 || string(prompt) != input.Task || socket != proxy.SocketPath() || token != proxy.LocalBearerToken() {
						t.Error("provider started before readiness or lost prepared binding")
					}
					mu.Unlock()
					if strings.HasPrefix(mode, "cancelled") {
						cancel()
					}
					if mode == "provider before effect" {
						return codex.Result{}, errors.New("synthetic pre-effect failure")
					}
					if mode != "cancelled broker" {
						if err := onActivity(runtimecontract.RuntimeActivity{ToolCall: &result.ToolCalls[0]}); err != nil {
							return result, err
						}
					}
					if strings.Contains(mode, "broker") {
						if mode == "cancelled broker" {
							result.ToolCalls = nil
						}
						return result, errors.New("synthetic credential commit failure")
					}
					return result, nil
				},
				checkWorkspace: func(context.Context) error { return nil },
			}
			if err := runTurn(ctx, input, client, func() { ready = true }, runtime); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			wantExecutions := 1
			if mode == "preparation" {
				wantExecutions = 0
			}
			wantTimeline := 1
			if mode == "preparation" || mode == "provider before effect" || strings.HasPrefix(mode, "cancelled") {
				wantTimeline = 0
			}
			if executions != wantExecutions || progress != wantExecutions || completions != 2 || timeline != wantTimeline ||
				cleaned != (wantExecutions == 1) || proxy.closed != (wantExecutions == 1) {
				t.Fatal("turn retried provider, lost timeline, terminal delivery or cleanup")
			}
		})
	}
}

func TestExecutedTurnRejectsInvalidArchiveBindings(t *testing.T) {
	for _, mode := range []string{"partial", "foreign session", "unsafe path", "invalid digest", "zero size", "oversized", "unknown outcome", "terminal failed", "invalid usage", "success partial"} {
		t.Run(mode, func(t *testing.T) {
			input := model.Input{RuntimeRevisionDigest: strings.Repeat("a", 64), Attempt: 3, LeaseRef: "lease_fixture"}
			result := codex.Result{Outcome: "SUCCEEDED", FinalMessage: "synthetic result", Usage: runtimecontract.TokenUsage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5},
				SessionID:           "00000000-0000-4000-8000-000000000001",
				ArchiveRelativePath: ".kodex/state/codex-home/sessions/2026/10/07/rollout-2026-10-07T00-00-00-00000000-0000-4000-8000-000000000001.jsonl",
				ArchiveSHA256:       strings.Repeat("d", 64), ArchiveSizeBytes: 26276}
			check := func(context.Context) error { return errors.New("synthetic workspace rejection") }
			wantCode := "RUNTIME_INPUT_INVALID"
			switch mode {
			case "partial":
				result.ArchiveSHA256 = ""
			case "foreign session":
				result.SessionID = "00000000-0000-4000-8000-000000000002"
			case "unsafe path":
				result.ArchiveRelativePath = "../rollout.jsonl"
			case "invalid digest":
				result.ArchiveSHA256 = strings.Repeat("z", 64)
			case "zero size":
				result.ArchiveSizeBytes = 0
			case "oversized":
				result.ArchiveSizeBytes = runtimecontract.MaximumSessionSourceBytes + 1
			case "unknown outcome":
				result.Outcome = "UNKNOWN"
			case "terminal failed":
				result.Outcome, result.FailureCode, result.ArchiveSHA256 = "FAILED", "usage_limit_exceeded", ""
				check = func(context.Context) error { return nil }
			case "invalid usage":
				result.Usage.InputTokens = -1
			case "success partial":
				result.ArchiveSHA256 = ""
				check = func(context.Context) error { return nil }
				wantCode = "PROVIDER_RESPONSE_INVALID"
			}
			calls := 0
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var payload runtimecontract.RunnerCompletionRequest
				if json.NewDecoder(r.Body).Decode(&payload) != nil || payload.Validate() != nil || payload.Success || payload.SafeErrorCode != wantCode || payload.Usage != result.Usage ||
					payload.CodexSessionID != "" || payload.ArchiveRelativePath != "" || payload.ArchiveSHA256 != "" || payload.ArchiveSizeBytes != 0 {
					t.Error("invalid result escaped the failure boundary")
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAnyClientCert}
			server.StartTLS()
			defer server.Close()
			client := completionTestClient(t, &input, server)
			err := completeExecutedTurn(t.Context(), input, client, result, check)
			wantRejection := mode == "terminal failed" || mode == "invalid usage"
			if (err != nil) != wantRejection || calls != map[bool]int{true: 0, false: 1}[wantRejection] {
				t.Fatal("completion validation or terminal failure semantics changed")
			}
		})
	}
}
