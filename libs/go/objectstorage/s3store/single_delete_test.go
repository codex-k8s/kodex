package s3store

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
)

// TestDeleteOnceNeverRetriesUnknownSDKOutcome verifies actual request counts for retryable 503
// and disconnect after the DELETE request arrived, while preserving the exact version selector.
// TestDeleteOnceNeverRetriesUnknownSDKOutcome проверяет actual request counts при retryable503
// и разрыве после получения DELETE, сохраняя exact version selector.
func TestDeleteOnceNeverRetriesUnknownSDKOutcome(t *testing.T) {
	for _, name := range []string{"http-503", "disconnect"} {
		t.Run(name, func(t *testing.T) {
			var attempts atomic.Int64
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete || r.URL.Query().Get("versionId") != "exact-version" {
					t.Error("unexpected synthetic delete selector")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				attempts.Add(1)
				if name == "http-503" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				connection, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error("synthetic delete shutdown failed")
					return
				}
				_ = connection.Close()
			}))
			defer endpoint.Close()
			store, err := New(t.Context(), Config{Endpoint: endpoint.URL, Region: "us-east-1", Bucket: "synthetic", AccessKeyID: "synthetic-key", SecretKey: "synthetic-secret", UsePathStyle: true})
			if err != nil {
				t.Fatal("synthetic S3 setup failed")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			if err := store.DeleteOnce(ctx, "synthetic/one-delete", "exact-version"); err != objectstorage.ErrUnavailable || attempts.Load() != 1 {
				t.Fatalf("unknown DeleteOnce outcome retried: unavailable=%t attempts=%d", err == objectstorage.ErrUnavailable, attempts.Load())
			}
		})
	}
}

// TestOrdinaryDeleteRetainsSDKRetry proves the optional mode does not change unrelated callers.
// TestOrdinaryDeleteRetainsSDKRetry доказывает, что optional mode не меняет посторонних callers.
func TestOrdinaryDeleteRetainsSDKRetry(t *testing.T) {
	var attempts atomic.Int64
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Error("unexpected synthetic method")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer endpoint.Close()
	store, err := New(t.Context(), Config{Endpoint: endpoint.URL, Region: "us-east-1", Bucket: "synthetic", AccessKeyID: "synthetic-key", SecretKey: "synthetic-secret", UsePathStyle: true})
	if err != nil {
		t.Fatal("synthetic S3 setup failed")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := store.Delete(ctx, "synthetic/ordinary-delete", "exact-version"); err != nil || attempts.Load() != 2 {
		t.Fatalf("ordinary Delete retry policy changed: failed=%t attempts=%d", err != nil, attempts.Load())
	}
}

// TestDeleteOnceRejectsUnversionedSelector proves invalid exact-mode inputs never reach storage.
// TestDeleteOnceRejectsUnversionedSelector доказывает, что invalid exact-mode inputs не достигают storage.
func TestDeleteOnceRejectsUnversionedSelector(t *testing.T) {
	var calls atomic.Int64
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(http.StatusNoContent) }))
	defer endpoint.Close()
	store, err := New(t.Context(), Config{Endpoint: endpoint.URL, Region: "us-east-1", Bucket: "synthetic", AccessKeyID: "synthetic-key", SecretKey: "synthetic-secret", UsePathStyle: true})
	if err != nil {
		t.Fatal("synthetic S3 setup failed")
	}
	for _, version := range []string{"", "null", "NULL", "bad\nversion"} {
		if err := store.DeleteOnce(t.Context(), "synthetic/invalid-delete", version); err != objectstorage.ErrInvalid {
			t.Fatal("invalid exact delete selector was accepted")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid exact delete reached server")
	}
}

// TestDeleteOnceRejectsRedirect verifies one SDK attempt cannot hide additional HTTP DELETEs;
// ordinary deletion retains the SDK's existing method-preserving redirect behavior.
// TestDeleteOnceRejectsRedirect проверяет, что одна SDK attempt не скрывает дополнительные HTTP
// DELETE; обычное удаление сохраняет прежний method-preserving redirect behavior SDK.
func TestDeleteOnceRejectsRedirect(t *testing.T) {
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int64
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodDelete || r.URL.Query().Get("versionId") != "exact-version" {
					t.Error("redirect changed exact delete selector")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if r.URL.Query().Get("redirected") == "yes" {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				location := *r.URL
				query := location.Query()
				query.Set("redirected", "yes")
				location.RawQuery = query.Encode()
				w.Header().Set("Location", location.String())
				w.WriteHeader(status)
			}))
			defer endpoint.Close()
			store, err := New(t.Context(), Config{Endpoint: endpoint.URL, Region: "us-east-1", Bucket: "synthetic", AccessKeyID: "synthetic-key", SecretKey: "synthetic-secret", UsePathStyle: true})
			if err != nil {
				t.Fatal("synthetic S3 setup failed")
			}
			if err := store.DeleteOnce(t.Context(), "synthetic/no-redirect", "exact-version"); err != objectstorage.ErrUnavailable || calls.Load() != 1 {
				t.Fatalf("exact delete followed redirect: failed=%t calls=%d", err != nil, calls.Load())
			}
			calls.Store(0)
			if err := store.Delete(t.Context(), "synthetic/ordinary-redirect", "exact-version"); err != nil || calls.Load() != 2 {
				t.Fatalf("ordinary redirect policy changed: failed=%t calls=%d", err != nil, calls.Load())
			}
		})
	}
}

// opaqueDeleteHTTPClient represents a preexisting adapter whose redirect policy cannot be inspected.
// opaqueDeleteHTTPClient моделирует existing adapter с недоступной для проверки redirect policy.
type opaqueDeleteHTTPClient struct{ client *http.Client }

// Do preserves the ordinary configured HTTP transport for compatibility checks.
// Do сохраняет ordinary configured HTTP transport для compatibility checks.
func (client opaqueDeleteHTTPClient) Do(request *http.Request) (*http.Response, error) {
	return client.client.Do(request)
}

// TestDeleteOnceRefusesOpaqueTransportBeforeIO keeps ordinary custom adapters usable while
// refusing irreversible admission when their one-request behavior cannot be attested.
// TestDeleteOnceRefusesOpaqueTransportBeforeIO сохраняет ordinary custom adapters доступными,
// отклоняя irreversible admission без проверяемого one-request behavior.
func TestDeleteOnceRefusesOpaqueTransportBeforeIO(t *testing.T) {
	var calls atomic.Int64
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer endpoint.Close()
	store, err := New(t.Context(), Config{Endpoint: endpoint.URL, Region: "us-east-1", Bucket: "synthetic", AccessKeyID: "synthetic-key", SecretKey: "synthetic-secret", UsePathStyle: true, HTTPClient: opaqueDeleteHTTPClient{client: endpoint.Client()}})
	if err != nil {
		t.Fatal("ordinary custom client was rejected")
	}
	if store.CheckDeleteOnce(t.Context()) != objectstorage.ErrUnavailable || store.DeleteOnce(t.Context(), "synthetic/opaque", "exact-version") != objectstorage.ErrUnavailable || calls.Load() != 0 {
		t.Fatal("opaque HTTP client consumed an irreversible request")
	}
	if err := store.Delete(t.Context(), "synthetic/ordinary", "exact-version"); err != nil || calls.Load() != 1 {
		t.Fatal("ordinary custom HTTP client behavior changed")
	}
}

// TestExactDeleteViewSharesConfiguredTransport ensures no second TLS/connection-pool path is created.
// TestExactDeleteViewSharesConfiguredTransport проверяет отсутствие второго TLS/connection-pool пути.
func TestExactDeleteViewSharesConfiguredTransport(t *testing.T) {
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()
	base := &http.Client{Transport: transport, Timeout: 7 * time.Second}
	ordinary, exact := exactDeleteHTTPClient(base)
	view, ok := exact.(*exactDeleteClient)
	if ordinary != base || !ok || view.client.Transport != base.Transport || view.client.Timeout != base.Timeout || base.CheckRedirect != nil || view.client.CheckRedirect == nil {
		t.Fatal("exact delete changed the configured transport or ordinary redirect policy")
	}
}

// opaqueDeleteRoundTripper is an uninspectable custom transport with ordinary request semantics.
// opaqueDeleteRoundTripper моделирует непроверяемый custom transport с ordinary request semantics.
type opaqueDeleteRoundTripper struct{ transport http.RoundTripper }

// RoundTrip delegates the ordinary request without claiming a one-request guarantee.
// RoundTrip передаёт ordinary request без заявления one-request guarantee.
func (transport opaqueDeleteRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport.transport.RoundTrip(request)
}

// TestExactDeleteRefusesOpaqueRoundTripper prevents a custom transport from hiding its own retry.
// TestExactDeleteRefusesOpaqueRoundTripper запрещает custom transport скрывать собственный retry.
func TestExactDeleteRefusesOpaqueRoundTripper(t *testing.T) {
	base := &http.Client{Transport: opaqueDeleteRoundTripper{transport: http.DefaultTransport}}
	ordinary, exact := exactDeleteHTTPClient(base)
	if ordinary != base || exact != nil {
		t.Fatal("opaque transport advertised one physical deletion request")
	}
}
