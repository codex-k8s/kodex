package oidcverifier

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

func TestBoundedKeySetStopsAfterLastKnownGoodWindow(t *testing.T) {
	t.Parallel()

	body := testJWKS(t, "key-1")
	var mutex sync.RWMutex
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		mutex.RLock()
		defer mutex.RUnlock()
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(body)
	}))
	defer server.Close()
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	set := newBoundedKeySet(server.Client(), server.URL)
	set.now = func() time.Time { return now }
	if err := set.Refresh(t.Context()); err != nil {
		t.Fatalf("initial refresh: %v", err)
	}
	server.Close()
	if err := set.Refresh(t.Context()); err == nil {
		t.Fatal("network failure was not reported")
	}
	deadline, degraded := set.DegradedDeadline()
	if !degraded || !deadline.Equal(now.Add(lastKnownGoodWindow)) {
		t.Fatalf("degraded deadline = %v %t", deadline, degraded)
	}
	now = now.Add(lastKnownGoodWindow + time.Second)
	if _, err := set.VerifySignature(t.Context(), "not-a-token"); err == nil {
		t.Fatal("expired last-known-good keys were accepted")
	}
}

func TestBoundedKeySetFailsClosedOnMalformedSnapshot(t *testing.T) {
	t.Parallel()

	body := testJWKS(t, "key-1")
	var mutex sync.RWMutex
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		mutex.RLock()
		defer mutex.RUnlock()
		_, _ = writer.Write(body)
	}))
	defer server.Close()
	set := newBoundedKeySet(server.Client(), server.URL)
	if err := set.Refresh(t.Context()); err != nil {
		t.Fatalf("initial refresh: %v", err)
	}
	mutex.Lock()
	body = []byte(`{"keys":"corrupt"}`)
	mutex.Unlock()
	if err := set.Refresh(t.Context()); err == nil {
		t.Fatal("malformed JWKS was accepted")
	}
	mutex.Lock()
	body = testJWKS(t, "key-2")
	mutex.Unlock()
	if err := set.Refresh(t.Context()); err == nil {
		t.Fatal("blocked JWKS state recovered without controlled restart")
	}
}

func testJWKS(t *testing.T, keyID string) []byte {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key: &privateKey.PublicKey, KeyID: keyID, Algorithm: string(jose.RS256), Use: "sig",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
