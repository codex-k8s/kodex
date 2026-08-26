package oidcverifier

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

func TestBoundedKeySetRejectsVerificationBeforeFirstSnapshot(t *testing.T) {
	t.Parallel()

	set := newBoundedKeySet(&http.Client{Timeout: time.Millisecond}, "http://127.0.0.1:1/jwks")
	if err := set.Refresh(t.Context()); err == nil {
		t.Fatal("недоступный начальный JWKS был принят")
	}
	if err := set.verificationError(); !errors.Is(err, ErrSigningKeysUnavailable) {
		t.Fatalf("начальное отсутствие JWKS = %v, ожидается typed unavailable", err)
	}
}

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
	if _, err := set.VerifySignature(t.Context(), "not-a-token"); !errors.Is(err, ErrSigningKeysUnavailable) {
		t.Fatalf("истёкшие last-known-good ключи вернули %v, ожидается typed unavailable", err)
	}
}

func TestBoundedKeySetRepeatedFailuresDoNotExtendLastKnownGoodWindow(t *testing.T) {
	t.Parallel()

	body := testJWKS(t, "key-1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(body)
	}))
	startedAt := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	now := startedAt
	set := newBoundedKeySet(server.Client(), server.URL)
	set.now = func() time.Time { return now }
	if err := set.Refresh(t.Context()); err != nil {
		server.Close()
		t.Fatalf("initial refresh: %v", err)
	}
	server.Close()

	expectedDeadline := startedAt.Add(lastKnownGoodWindow)
	for _, elapsed := range []time.Duration{30 * time.Second, 90 * time.Second, 119 * time.Second} {
		now = startedAt.Add(elapsed)
		if err := set.Refresh(t.Context()); err == nil {
			t.Fatal("network failure was not reported")
		}
		deadline, degraded := set.DegradedDeadline()
		if !degraded || !deadline.Equal(expectedDeadline) {
			t.Fatalf("failure extended last-known-good deadline to %v", deadline)
		}
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

func TestBoundedKeySetAcceptsKeycloakMixedUseJWKS(t *testing.T) {
	t.Parallel()

	signingKey := testPublicJWK(t, "signing-key", string(jose.RS256), "sig")
	encryptionKey := testPublicJWK(t, "encryption-key", string(jose.RSA_OAEP), "enc")
	body, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{signingKey, encryptionKey}})
	if err != nil {
		t.Fatalf("marshal mixed-use JWKS: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(body)
	}))
	defer server.Close()
	set := newBoundedKeySet(server.Client(), server.URL)
	if err := set.Refresh(t.Context()); err != nil {
		t.Fatalf("mixed-use Keycloak JWKS был отклонен: %v", err)
	}
	set.mutex.RLock()
	defer set.mutex.RUnlock()
	if len(set.keys.Keys) != 1 || set.keys.Keys[0].KeyID != signingKey.KeyID ||
		set.keys.Keys[0].Algorithm != string(jose.RS256) || set.keys.Keys[0].Use != "sig" {
		t.Fatalf("материализован неожиданный signing key set: %+v", set.keys.Keys)
	}
}

func TestBoundedKeySetRejectsJWKSWithoutSupportedSigningKey(t *testing.T) {
	t.Parallel()

	body, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
		testPublicJWK(t, "encryption-key", string(jose.RSA_OAEP), "enc"),
	}})
	if err != nil {
		t.Fatalf("marshal encryption-only JWKS: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(body)
	}))
	defer server.Close()
	set := newBoundedKeySet(server.Client(), server.URL)
	if err := set.Refresh(t.Context()); err == nil {
		t.Fatal("JWKS без поддерживаемого signing key был принят")
	}
	if err := set.verificationError(); !errors.Is(err, ErrSigningKeysUnavailable) {
		t.Fatalf("отсутствие signing key вернуло %v, ожидается typed unavailable", err)
	}
}

func TestBoundedKeySetRejectsDuplicateKeyIDAcrossUses(t *testing.T) {
	t.Parallel()

	body, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
		testPublicJWK(t, "shared-key", string(jose.RS256), "sig"),
		testPublicJWK(t, "shared-key", string(jose.RSA_OAEP), "enc"),
	}})
	if err != nil {
		t.Fatalf("marshal duplicate-kid JWKS: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(body)
	}))
	defer server.Close()
	set := newBoundedKeySet(server.Client(), server.URL)
	if err := set.Refresh(t.Context()); err == nil {
		t.Fatal("duplicate kid между signing и encryption keys был принят")
	}
}

func TestBoundedKeySetRejectsMalformedSupportedSigningKey(t *testing.T) {
	t.Parallel()

	body, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key: []byte("not-an-rsa-public-key"), KeyID: "malformed-signing-key",
		Algorithm: string(jose.RS256), Use: "sig",
	}}})
	if err != nil {
		t.Fatalf("marshal malformed signing JWKS: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(body)
	}))
	defer server.Close()
	set := newBoundedKeySet(server.Client(), server.URL)
	if err := set.Refresh(t.Context()); err == nil {
		t.Fatal("поврежденный поддерживаемый signing key был принят")
	}
}

func testJWKS(t *testing.T, keyID string) []byte {
	t.Helper()
	raw, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
		testPublicJWK(t, keyID, string(jose.RS256), "sig"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func testPublicJWK(t *testing.T, keyID, algorithm, use string) jose.JSONWebKey {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return jose.JSONWebKey{Key: &privateKey.PublicKey, KeyID: keyID, Algorithm: algorithm, Use: use}
}
