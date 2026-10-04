package codex

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

func TestExecuteProviderTurnSkipsRefreshForUnchangedAPIKey(t *testing.T) {
	input, authPath := providerTurnFixture(t, []byte(`{"auth_mode":"apikey","OPENAI_API_KEY":"test-key"}`))
	called := false
	want := Result{Outcome: "SUCCEEDED"}
	got, err := executeProviderTurn(context.Background(), input, []byte("task"), strings.Repeat("a", 64),
		func(context.Context, model.Input, []byte, string) (Result, error) { return want, nil },
		func(context.Context, model.Input, runtimecontract.RunnerProviderCredentialRefreshRequest) error {
			called = true
			return nil
		})
	if err != nil {
		t.Fatalf("executeProviderTurn() error = %v", err)
	}
	if got.Outcome != want.Outcome || called {
		t.Fatalf("executeProviderTurn() = %#v, callback called = %v", got, called)
	}
	assertRemoved(t, authPath)
}

func TestExecuteProviderTurnRejectsChangedAPIKeyWithoutRelay(t *testing.T) {
	original := []byte(`{"auth_mode":"apikey","OPENAI_API_KEY":"old-key"}`)
	changed := []byte(`{"auth_mode":"apikey","OPENAI_API_KEY":"new-key"}`)
	input, authPath := providerTurnFixture(t, original)
	called := false
	_, err := executeProviderTurn(context.Background(), input, []byte("task"), strings.Repeat("a", 64),
		func(context.Context, model.Input, []byte, string) (Result, error) {
			if err := os.WriteFile(authPath, changed, 0o600); err != nil {
				t.Fatal(err)
			}
			return Result{Outcome: "SUCCEEDED"}, nil
		}, func(context.Context, model.Input, runtimecontract.RunnerProviderCredentialRefreshRequest) error {
			called = true
			return nil
		})
	if err == nil || err.Error() != "provider API-key authentication changed unexpectedly" {
		t.Fatalf("executeProviderTurn() error = %v", err)
	}
	if called {
		t.Fatal("changed API-key authentication reached OAuth credential relay")
	}
	assertRemoved(t, authPath)
}

func TestExecuteProviderTurnCommitsChangedAuthenticationAfterSafeFailure(t *testing.T) {
	original := []byte(`{"auth_mode":"chatgpt","tokens":{"access_token":"old","refresh_token":"old"}}`)
	rotated := []byte(`{"auth_mode":"chatgpt","tokens":{"access_token":"new","refresh_token":"new"}}`)
	input, authPath := providerTurnFixture(t, original)
	var captured runtimecontract.RunnerProviderCredentialRefreshRequest
	got, err := executeProviderTurn(context.Background(), input, []byte("task"), strings.Repeat("a", 64),
		func(context.Context, model.Input, []byte, string) (Result, error) {
			if err := os.WriteFile(authPath, rotated, 0o600); err != nil {
				t.Fatal(err)
			}
			return Result{Outcome: "FAILED", FailureCode: "PROVIDER_REQUEST_REJECTED"}, nil
		}, func(_ context.Context, _ model.Input, payload runtimecontract.RunnerProviderCredentialRefreshRequest) error {
			captured = payload
			captured.Authentication = append([]byte(nil), payload.Authentication...)
			return nil
		})
	if err != nil || got.Outcome != "FAILED" || got.FailureCode != "PROVIDER_REQUEST_REJECTED" {
		t.Fatalf("executeProviderTurn() = %#v, %v", got, err)
	}
	if captured.RuntimeRevisionDigest != input.RuntimeRevisionDigest ||
		captured.PreviousCredentialRevisionRef != input.ProviderCredentialRef ||
		captured.PreviousContentSHA256 != input.ProviderCredentialSHA256 ||
		string(captured.Authentication) != string(rotated) {
		t.Fatal("captured refresh metadata does not match the rotated snapshot")
	}
	assertRemoved(t, authPath)
}

func TestExecuteProviderTurnCommitsRefreshAfterExecutionError(t *testing.T) {
	original := []byte(`{"auth_mode":"chatgpt","tokens":{"refresh_token":"old"}}`)
	rotated := []byte(`{"auth_mode":"chatgpt","tokens":{"refresh_token":"new"}}`)
	input, authPath := providerTurnFixture(t, original)
	executionErr := errors.New("ordinary execution failure")
	callbackCalled := false
	_, err := executeProviderTurn(context.Background(), input, []byte("task"), strings.Repeat("a", 64),
		func(context.Context, model.Input, []byte, string) (Result, error) {
			if err := os.WriteFile(authPath, rotated, 0o600); err != nil {
				t.Fatal(err)
			}
			return Result{}, executionErr
		}, func(_ context.Context, _ model.Input, payload runtimecontract.RunnerProviderCredentialRefreshRequest) error {
			callbackCalled = len(payload.Authentication) > 0
			return nil
		})
	if !errors.Is(err, executionErr) || !callbackCalled {
		t.Fatalf("executeProviderTurn() error = %v, callback called = %v", err, callbackCalled)
	}
	assertRemoved(t, authPath)
}

func TestExecuteProviderTurnFailsClosedWhenRelayFails(t *testing.T) {
	original := []byte(`{"auth_mode":"chatgpt","tokens":{"refresh_token":"old"}}`)
	rotated := []byte(`{"auth_mode":"chatgpt","tokens":{"refresh_token":"new"}}`)
	input, authPath := providerTurnFixture(t, original)
	_, err := executeProviderTurn(context.Background(), input, []byte("task"), strings.Repeat("a", 64),
		func(context.Context, model.Input, []byte, string) (Result, error) {
			if err := os.WriteFile(authPath, rotated, 0o600); err != nil {
				t.Fatal(err)
			}
			return Result{Outcome: "SUCCEEDED"}, nil
		}, func(context.Context, model.Input, runtimecontract.RunnerProviderCredentialRefreshRequest) error {
			return errors.New("relay failure")
		})
	if err == nil || err.Error() != "commit refreshed provider authentication" {
		t.Fatalf("executeProviderTurn() error = %v", err)
	}
	assertRemoved(t, authPath)
}

func TestProviderBrokerFailurePreservesSafeClass(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		failure providerBrokerFailure
		want    error
	}{
		{name: "authentication", err: ErrProviderAuthentication, failure: providerBrokerFailureAuthentication, want: ErrProviderAuthentication},
		{name: "authority", err: ErrAuthorityRequestUnsupported, failure: providerBrokerFailureAuthority, want: ErrAuthorityRequestUnsupported},
		{name: "mcp", err: ErrRequiredMCPUnavailable, failure: providerBrokerFailureMCP, want: ErrRequiredMCPUnavailable},
		{name: "configuration", err: ErrRuntimeProfile, failure: providerBrokerFailureConfiguration, want: ErrRuntimeProfile},
		{name: "provider", err: errors.New("transport"), failure: providerBrokerFailureProvider},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyProviderBrokerFailure(test.err); got != test.failure {
				t.Fatalf("classifyProviderBrokerFailure() = %q, want %q", got, test.failure)
			}
			err := providerBrokerError(test.failure)
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("providerBrokerError() = %v, want %v", err, test.want)
			}
			if err == nil {
				t.Fatal("providerBrokerError() returned nil")
			}
		})
	}
}

func TestProviderSafeFailureClassDoesNotExposeDiagnostics(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{name: "account schema", err: atProviderStage(providerStageAccountRead, errAccountReadResponseInvalid), want: "ACCOUNT_RESPONSE_SCHEMA"},
		{name: "authentication", err: atProviderStage(providerStageAccountRead, ErrProviderAuthentication), want: "AUTHENTICATION"},
		{name: "external diagnostic", err: errors.New("fixture secret and account details must stay private"), want: "PROVIDER"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := providerSafeFailureClass(test.err); got != test.want {
				t.Fatalf("safe provider failure class = %q, want %q", got, test.want)
			}
		})
	}
}

func TestProviderBrokerEarlyFailureLogsOnlySafeStage(t *testing.T) {
	var response bytes.Buffer
	var diagnostic bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&diagnostic)
	t.Cleanup(func() { log.SetOutput(previous) })

	secret := errors.New("sensitive-provider-detail")
	if err := writeProviderBrokerFailureAtStage(&response, providerStageHomePrepare, secret); err != nil {
		t.Fatalf("writeProviderBrokerFailureAtStage() error = %v", err)
	}
	if strings.Contains(diagnostic.String(), secret.Error()) || strings.Contains(response.String(), secret.Error()) {
		t.Fatal("provider failure exposed the original error")
	}
	if !strings.Contains(diagnostic.String(), "HOME_PREPARE") {
		t.Fatalf("safe diagnostic = %q", diagnostic.String())
	}
	var frame brokerFrame
	if err := json.Unmarshal(response.Bytes(), &frame); err != nil || frame.Version != providerBrokerVersion || frame.Kind != brokerFrameTerminal || frame.Terminal == nil || frame.Terminal.OK || frame.Terminal.Failure != providerBrokerFailureProvider {
		t.Fatalf("broker response = %#v, error = %v", frame, err)
	}
}

func TestProviderSafeFailureDetailsRemainClosed(t *testing.T) {
	var diagnostic bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&diagnostic)
	t.Cleanup(func() { log.SetOutput(previous) })
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{name: "RPC code", err: protocolError("account/read", json.RawMessage(`{"code":-32603,"message":"private-upstream-detail"}`)), want: "detail: RPC_ERROR; rpc_code: -32603"},
		{name: "notification", err: callFailure("NOTIFICATION_INVALID", errors.New("private-upstream-detail")), want: "detail: NOTIFICATION_INVALID; rpc_code: 0"},
		{name: "unknown detail", err: &appServerCallFailure{detail: "private-upstream-detail", code: 42, err: errors.New("private-upstream-detail")}, want: "detail: NONE; rpc_code: 0"},
		{name: "registered notification", err: &appServerCallFailure{detail: "NOTIFICATION_INVALID", notification: "account/updated", err: errors.New("private-upstream-detail")}, want: "notification: account/updated"},
		{name: "unknown notification", err: &appServerCallFailure{detail: "NOTIFICATION_INVALID", notification: "private-upstream-detail", err: errors.New("private-upstream-detail")}, want: "notification: UNKNOWN"},
		{name: "SDK diagnostic only method", err: notificationFailure("thread/attachment/updated", errors.New("Codex app-server notification method is not allowed")), want: "notification: thread/attachment/updated; account_read: NONE; notification_error: METHOD"},
		{name: "known parser category", err: notificationFailure("item/started", errors.New("Codex app-server tagged thread item is invalid")), want: "notification_error: ITEM"},
		{name: "unknown parser category", err: notificationFailure("item/started", errors.New("private-upstream-detail")), want: "notification_error: UNKNOWN"},
		{name: "injected category", err: &appServerCallFailure{detail: "NOTIFICATION_INVALID", notification: "item/started", notificationError: "private-upstream-detail", err: errors.New("private-upstream-detail")}, want: "notification_error: UNKNOWN"},
		{name: "known account failure", err: &appServerCallFailure{detail: "RPC_ERROR", code: -32603, accountRead: "DISCOVERY_FAILED", err: errors.New("private-upstream-detail")}, want: "account_read: DISCOVERY_FAILED"},
		{name: "unknown account failure", err: &appServerCallFailure{detail: "RPC_ERROR", code: -32603, accountRead: "private-upstream-detail", err: errors.New("private-upstream-detail")}, want: "account_read: UNKNOWN"},
		{name: "different RPC code", err: &appServerCallFailure{detail: "RPC_ERROR", code: -32602, accountRead: "DISCOVERY_FAILED", err: errors.New("private-upstream-detail")}, want: "account_read: NONE"},
	} {
		t.Run(test.name, func(t *testing.T) {
			diagnostic.Reset()
			logProviderSafeFailure(providerStageAccountRead, atProviderStage(providerStageAccountRead, test.err))
			if strings.Contains(diagnostic.String(), "private-upstream-detail") || !strings.Contains(diagnostic.String(), test.want) {
				t.Fatalf("unsafe or incomplete provider diagnostic: %q", diagnostic.String())
			}
		})
	}
}

func providerTurnFixture(t *testing.T, authentication []byte) (model.Input, string) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, ".kodex", "state", "codex-home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	authPath := filepath.Join(home, "auth.json")
	if err := os.WriteFile(authPath, authentication, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(authentication)
	return model.Input{
		LeaseRef:                 "lease_abcdefgh",
		RuntimeRevisionDigest:    strings.Repeat("a", 64),
		ProviderCredentialRef:    "pcr_abcdefgh",
		ProviderCredentialSHA256: hex.EncodeToString(digest[:]),
		WorkspaceRoot:            root,
		CodexHome:                home,
	}, authPath
}

func assertRemoved(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("authentication snapshot still exists: %v", err)
	}
}
