package oidcverifier

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestSigningKeyInitializationPreservesFailurePolicy observes real snapshot
// parsing across first failure, recovery, grace expiry and permanent corruption.
// The startup milestone must never replace the separate token eligibility check.
// TestSigningKeyInitializationPreservesFailurePolicy проверяет настоящий разбор
// снимка при первом отказе, восстановлении, истечении grace и повреждении.
// Факт инициализации не должен заменять отдельную проверку допуска токена.
func TestSigningKeyInitializationPreservesFailurePolicy(t *testing.T) {
	t.Parallel()
	var absent *Verifier
	if absent.SigningKeysInitialized() || (&Verifier{}).SigningKeysInitialized() {
		t.Fatal("missing verifier accepted initialization")
	}
	body := testJWKS(t, "initialization-key")
	var responseMode atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		switch responseMode.Load() {
		case 0:
			w.WriteHeader(http.StatusServiceUnavailable)
		case 1:
			_, _ = w.Write(body)
		case 2:
			_, _ = w.Write([]byte(`{"keys":"invalid"}`))
		}
	}))
	defer server.Close()
	set := newBoundedKeySet(server.Client(), server.URL)
	now := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	set.now = func() time.Time { return now }
	verifier := &Verifier{keys: set}
	if err := verifier.Refresh(t.Context()); err == nil || verifier.SigningKeysInitialized() {
		t.Fatal("initial failure accepted initialization")
	}
	responseMode.Store(1)
	if err := verifier.Refresh(t.Context()); err != nil || !verifier.SigningKeysInitialized() {
		t.Fatalf("valid first snapshot did not initialize: %v", err)
	}
	responseMode.Store(0)
	if err := verifier.Refresh(t.Context()); err == nil || !verifier.SigningKeysInitialized() {
		t.Fatal("later outage reset initialization")
	}
	if err := set.verificationError(); err != nil {
		t.Fatal("existing last-known-good policy changed")
	}
	now = now.Add(lastKnownGoodWindow + time.Second)
	if !verifier.SigningKeysInitialized() || !errors.Is(set.verificationError(), ErrSigningKeysUnavailable) {
		t.Fatal("initialization bypassed grace expiry")
	}
	responseMode.Store(2)
	if err := verifier.Refresh(t.Context()); err == nil || !verifier.SigningKeysInitialized() {
		t.Fatal("later corruption reset initialization")
	}
	responseMode.Store(1)
	if err := verifier.Refresh(t.Context()); err == nil || !errors.Is(set.verificationError(), ErrSigningKeysUnavailable) {
		t.Fatal("initialization bypassed permanent snapshot block")
	}
}

// TestSigningKeyInitializationRejectsInvalidFirstSnapshot keeps startup closed
// after a fatal first snapshot even if a later HTTP response contains valid keys.
// TestSigningKeyInitializationRejectsInvalidFirstSnapshot сохраняет закрытый
// старт после первого повреждённого снимка даже при последующем корректном HTTP.
func TestSigningKeyInitializationRejectsInvalidFirstSnapshot(t *testing.T) {
	t.Parallel()
	valid := testJWKS(t, "initialization-rejected")
	var recovered atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if recovered.Load() {
			_, _ = w.Write(valid)
		} else {
			_, _ = w.Write([]byte(`{"keys":[]}`))
		}
	}))
	defer server.Close()
	verifier := &Verifier{keys: newBoundedKeySet(server.Client(), server.URL)}
	if err := verifier.Refresh(t.Context()); err == nil || verifier.SigningKeysInitialized() {
		t.Fatal("invalid first snapshot initialized verifier")
	}
	recovered.Store(true)
	if err := verifier.Refresh(t.Context()); err == nil || verifier.SigningKeysInitialized() {
		t.Fatal("fatal initial failure recovered without controlled restart")
	}
}
