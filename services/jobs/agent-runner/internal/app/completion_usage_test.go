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
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/callback"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/codex"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	workspacepolicy "github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/workspace"
)

// completionTestClient creates temporary trust/client-identity/ticket files from the synthetic server, updates only the supplied fixture input and registers callback-client cleanup; the authentication bytes never come from live accounts.
// completionTestClient создаёт временные trust/client-identity/ticket файлы из синтетического сервера, меняет лишь переданный fixture input и регистрирует callback-client cleanup; auth bytes не берутся из живых аккаунтов.
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

// turnProxyFixture exposes inert local MCP strings and records bounded cleanup so turn tests can prove lifecycle ordering without running a proxy or provider.
// turnProxyFixture предоставляет неактивные local MCP strings и фиксирует ограниченное закрытие для проверки lifecycle ordering без прокси или провайдера.
type turnProxyFixture struct {
	t      *testing.T
	closed bool
}

// SocketPath returns only a synthetic endpoint marker used to assert prepared-turn argument routing.
// SocketPath возвращает лишь synthetic endpoint marker для проверки передачи аргументов prepared turn.
func (*turnProxyFixture) SocketPath() string { return "synthetic-mcp-socket" }

// LocalBearerToken returns an inert fixture marker, not a controller ticket or an actual local proxy capability.
// LocalBearerToken возвращает неактивный fixture marker, не controller ticket и не настоящую local proxy capability.
func (*turnProxyFixture) LocalBearerToken() string { return "synthetic-mcp-token" }

// Close checks the cleanup context is active and bounded to at most five seconds, then records closure for assertions after turn completion.
// Close проверяет активный cleanup context с пределом не более пяти секунд и фиксирует закрытие для assertions после завершения хода.
func (proxy *turnProxyFixture) Close(ctx context.Context) error {
	deadline, bounded := ctx.Deadline()
	if ctx.Err() != nil || !bounded || time.Until(deadline) > 5*time.Second {
		proxy.t.Error("proxy cleanup lost its independent bounded context")
	}
	proxy.closed = true
	return nil
}

// TestWorkspaceDiagnosticCompletion checks the authenticated callback with complete
// execution pins, preserving measured usage and the guard-selected failure cause.
// TestWorkspaceDiagnosticCompletion проверяет авторизованный callback с полными
// привязками выполнения, сохранением расхода и причины отказа файловой проверки.
func TestWorkspaceDiagnosticCompletion(t *testing.T) {
	input := model.Input{RuntimeRevisionDigest: strings.Repeat("a", 64), InputDigest: strings.Repeat("b", 64),
		ExecutionBindingDigest: strings.Repeat("c", 64), SessionRef: "ses_fixture1", TurnRef: "trn_fixture1", Attempt: 3, LeaseRef: "lease_fixture"}
	result := codex.Result{Outcome: "SUCCEEDED", FinalMessage: "review completed", Usage: runtimecontract.TokenUsage{InputTokens: 2, OutputTokens: 1, TotalTokens: 3}}
	calls := 0
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var payload runtimecontract.RunnerCompletionRequest
		if json.NewDecoder(r.Body).Decode(&payload) != nil || payload.Validate() != nil || payload.Success || payload.SafeErrorCode != "RUNTIME_ARTIFACT_INVALID" || payload.Usage != result.Usage {
			t.Error("workspace failure lost its completion or measured usage")
		}
		diagnostic := payload.ProviderDiagnostic
		if diagnostic == nil || diagnostic.Class != "WORKSPACE" || diagnostic.Stage != "CHECK_WORKSPACE" || diagnostic.Detail != "QUOTA_EXCEEDED" ||
			!diagnostic.Matches(input) {
			t.Error("workspace callback lost the bound guard diagnostic")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAnyClientCert}
	server.StartTLS()
	defer server.Close()
	client := completionTestClient(t, &input, server)
	check := func(context.Context) error {
		return &workspacepolicy.Denial{Reason: runtimecontract.RuntimeWorkspaceQuotaExceeded}
	}
	if err := completeExecutedTurn(t.Context(), input, client, result, check, nil); err != nil || calls != 1 {
		t.Fatalf("workspace completion failed: calls=%d err=%v", calls, err)
	}
}

// TestNativeFailureCompletionPreservesUsageAndExactReceipt checks each post-execution callback path against real TLS and a lost first acknowledgement.
// TestNativeFailureCompletionPreservesUsageAndExactReceipt проверяет callback после исполнения через настоящий TLS и потерянный первый acknowledgement.
func TestNativeFailureCompletionPreservesUsageAndExactReceipt(t *testing.T) {
	for _, mode := range []string{"cancel", "workspace", "provider", "missing checkpoint", "forged checkpoint"} {
		t.Run(mode, func(t *testing.T) {
			input := model.Input{Provider: "opencode", RuntimeRevisionDigest: strings.Repeat("a", 64), Attempt: 3, LeaseRef: "lease_fixture"}
			usage := runtimecontract.TokenUsage{InputTokens: 100, CachedInputTokens: 20, OutputTokens: 50, TotalTokens: 150}
			result := codex.Result{Outcome: "SUCCEEDED", FinalMessage: "готово", Usage: usage, UsageCompleteness: codex.UsageComplete}
			if mode == "provider" {
				result.Outcome, result.FailureCode = "FAILED", "provider_interrupted"
			}
			if mode == "forged checkpoint" {
				result.NativeSession = &runtimecontract.NativeSessionCheckpoint{SessionID: "ses_forged"}
			}
			var receipt []byte
			attempts := 0
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts++
				raw, _ := io.ReadAll(r.Body)
				var payload runtimecontract.RunnerCompletionRequest
				expectedUsage := usage
				expectedUsage.Completeness = runtimecontract.UsageComplete
				if json.Unmarshal(raw, &payload) != nil || payload.Validate() != nil || payload.Success || payload.Usage != expectedUsage || payload.NativeSession != nil || payload.Attempt != 3 ||
					r.Method != http.MethodPost || r.URL.Path != "/v1/executions/lease_fixture/complete" || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("c", 64) || r.TLS == nil || len(r.TLS.PeerCertificates) != 1 {
					t.Error("failure callback lost measurements, identity or checkpoint boundary")
				}
				if receipt == nil {
					receipt = append([]byte(nil), raw...)
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				if !bytes.Equal(receipt, raw) {
					t.Error("retry changed completion receipt")
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAnyClientCert}
			server.StartTLS()
			defer server.Close()
			client := completionTestClient(t, &input, server)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var err error
			switch mode {
			case "cancel":
				cancel()
				err = completeMeasuredFailure(ctx, input, client, result, "RUNTIME_CANCELLED")
			case "workspace":
				err = completeExecutedTurn(ctx, input, client, result, func(context.Context) error { return errors.New("synthetic workspace denial") }, nil)
			case "provider":
				err = completeResultFailure(ctx, input, client, result, "i18n:PROVIDER_RESPONSE_INVALID")
			default:
				err = completeExecutedTurn(ctx, input, client, result, func(context.Context) error { return nil }, nil)
			}
			if err != nil || attempts != 2 {
				t.Fatalf("completion retry failed: attempts=%d err=%v", attempts, err)
			}
		})
	}
}
