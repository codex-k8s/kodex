package s3store

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
)

// TestReadRangeActualTLSKeepsConditionalVersionAndSingleAttempt exercises the real pinned SDK.
// A server that ignores Range or fails the read cannot cause a hidden retry or successful delivery.
// TestReadRangeActualTLSKeepsConditionalVersionAndSingleAttempt проверяет actual pinned SDK.
// Игнорирование Range либо read failure не вызывает hidden retry либо successful delivery.
func TestReadRangeActualTLSKeepsConditionalVersionAndSingleAttempt(t *testing.T) {
	for _, mode := range []string{"valid", "ignored", "unavailable", "expired"} {
		t.Run(mode, func(t *testing.T) {
			input, _, _ := rangeFixture()
			var calls atomic.Int32
			joined := make(chan struct{}, 1)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				defer func() { joined <- struct{}{} }()
				if r.Method != http.MethodGet || r.URL.Path != "/fixture/"+input.Object.Key || r.URL.Query().Get("versionId") != input.Object.VersionID || r.Header.Get("Range") != "bytes=3-5" || r.Header.Get("If-Match") != "\"etag-1\"" {
					t.Error("actual SDK request lost immutable range pins")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if mode == "unavailable" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				w.Header().Set("ETag", "\"etag-1\"")
				w.Header().Set("x-amz-version-id", input.Object.VersionID)
				w.Header().Set("x-amz-meta-"+digestMetadataKey, input.Object.Digest)
				if mode == "ignored" {
					w.Header().Set("Content-Length", "10")
					_, _ = w.Write([]byte("0123456789"))
					return
				}
				w.Header().Set("Content-Range", "bytes 3-5/10")
				w.Header().Set("Content-Length", "3")
				w.WriteHeader(http.StatusPartialContent)
				if mode == "expired" {
					w.(http.Flusher).Flush()
					<-r.Context().Done()
					return
				}
				_, _ = w.Write([]byte("345"))
			}))
			t.Cleanup(server.Close)
			transport := server.Client()
			store, err := New(t.Context(), Config{Endpoint: server.URL, Region: "test", Bucket: "fixture", UsePathStyle: true, AccessKeyID: "synthetic-access", SecretKey: "synthetic-secret", HTTPClient: transport})
			if err != nil {
				t.Fatal("construct range fixture")
			}
			budget := 3 * time.Second
			if mode == "expired" {
				budget = 100 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(t.Context(), budget)
			defer cancel()
			payload, err := store.ReadRange(ctx, input)
			if mode == "valid" {
				if err != nil || string(payload) != "345" {
					t.Fatal("valid actual SDK range failed", err)
				}
			} else if err == nil || payload != nil {
				t.Fatal("invalid actual SDK range returned bytes")
			}
			clear(payload)
			select {
			case <-joined:
			case <-time.After(time.Second):
				t.Fatal("actual range handler did not join")
			}
			if calls.Load() != 1 {
				t.Fatal("actual SDK retried range")
			}
			transport.CloseIdleConnections()
		})
	}
}

// TestReadRangeClosesErrorReply protects private body cleanup even for a partial SDK error result.
// TestReadRangeClosesErrorReply проверяет private body cleanup даже при partial SDK error result.
func TestReadRangeClosesErrorReply(t *testing.T) {
	input, reply, body := rangeFixture()
	client := &rangeAPI{reply: reply, err: errors.New("synthetic remote failure")}
	got, err := (&Store{client: client, bucket: "fixture"}).ReadRange(t.Context(), input)
	if !errors.Is(err, objectstorage.ErrUnavailable) || got != nil || !body.closed || client.calls != 1 {
		t.Fatal("partial SDK error retained private body")
	}
}
