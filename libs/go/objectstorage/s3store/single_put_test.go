package s3store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
)

// 503 и обрыв после принятия тела являются неизвестным исходом записи:
// отсутствие receipt не разрешает SDK отправить второй versioned Put.
func TestPutUnknownOutcomeIsNeverRetriedBySDK(t *testing.T) {
	for _, name := range []string{"http-503", "transport-after-body"} {
		t.Run(name, func(t *testing.T) {
			var writes atomic.Int64
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut {
					t.Error("unknown Put outcome invoked readback")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				body, err := io.ReadAll(io.LimitReader(r.Body, 4))
				if err != nil || string(body) != "abc" {
					t.Error("synthetic server did not receive exact body")
				}
				writes.Add(1)
				if name == "http-503" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error("synthetic connection shutdown failed")
					return
				}
				_ = conn.Close()
			}))
			defer endpoint.Close()
			store, err := New(t.Context(), Config{Endpoint: endpoint.URL, Region: "us-east-1", Bucket: "synthetic", AccessKeyID: "synthetic-key", SecretKey: "synthetic-secret", UsePathStyle: true})
			if err != nil {
				t.Fatal("synthetic S3 setup failed")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			digest := sha256.Sum256([]byte("abc"))
			receipt, err := store.Put(ctx, objectstorage.PutInput{Key: "synthetic/one-intent", MediaType: "text/plain", Digest: fmt.Sprintf("sha256:%x", digest[:]), SizeBytes: 3, Body: bytes.NewReader([]byte("abc"))})
			if err != objectstorage.ErrUnavailable || receipt != (objectstorage.Receipt{}) || writes.Load() != 1 {
				t.Fatalf("unknown Put retried or published receipt: unavailable=%t writes=%d receipt_empty=%t", err == objectstorage.ErrUnavailable, writes.Load(), receipt == (objectstorage.Receipt{}))
			}
		})
	}
}

func TestHeadRetainsSDKReadRetry(t *testing.T) {
	var reads atomic.Int64
	digest := "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Error("unexpected synthetic method")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if reads.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Length", "3")
		w.Header().Set("x-amz-version-id", "exact-version")
		w.Header().Set("x-amz-meta-kodex-sha256", digest)
		w.Header().Set("ETag", `"exact-etag"`)
	}))
	defer endpoint.Close()
	store, err := New(t.Context(), Config{Endpoint: endpoint.URL, Region: "us-east-1", Bucket: "synthetic", AccessKeyID: "synthetic-key", SecretKey: "synthetic-secret", UsePathStyle: true})
	if err != nil {
		t.Fatal("synthetic S3 setup failed")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	receipt, err := store.Head(ctx, "synthetic/one-intent", "exact-version")
	if err != nil || reads.Load() != 2 || receipt.VersionID != "exact-version" || receipt.Digest != digest {
		t.Fatalf("read retry changed: success=%t reads=%d", err == nil, reads.Load())
	}
}
