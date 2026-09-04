package codex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
