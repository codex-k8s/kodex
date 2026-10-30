package callback

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestArtifactInitializationPreservesSocketDeadline exercises an actual response read deadline,
// distinct from context cancellation. One failed GET writes no bytes and exposes no ticket or URL.
// TestArtifactInitializationPreservesSocketDeadline проверяет настоящий deadline чтения сокета,
// отличный от отмены context. Единственный отказавший GET не пишет байты и не раскрывает ticket либо URL.
func TestArtifactInitializationPreservesSocketDeadline(t *testing.T) {
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		<-request.Context().Done()
	}))
	defer upstream.Close()
	base, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal("parse local artifact fixture")
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		connection, err := (&net.Dialer{}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		if err := connection.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
			_ = connection.Close()
			return nil, err
		}
		return connection, nil
	}}
	defer transport.CloseIdleConnections()
	client := &Client{files: &http.Client{Transport: transport}, base: base, token: "PRIVATE_EXECUTION_TICKET"}
	var destination bytes.Buffer
	err = client.WriteArtifact(t.Context(), runtimecontract.RunnerInput{LeaseRef: "lease_fixture"},
		runtimecontract.RunnerInputArtifact{Ref: "artifact_fixture", MediaType: "text/plain"}, &destination)
	stage, reason := runtimecontract.InitializationErrorDetails(err)
	if !errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) ||
		stage != "ARTIFACT_TRANSFER" || reason != "DEADLINE_EXCEEDED" {
		t.Fatalf("socket deadline misclassified: stage=%s reason=%s", stage, reason)
	}
	if requests.Load() != 1 || destination.Len() != 0 || strings.Contains(err.Error(), client.token) || strings.Contains(err.Error(), upstream.URL) {
		t.Fatal("failed download retried, wrote bytes or exposed private transport data")
	}
}

// TestArtifactInitializationReasonsFollowActualHTTPAndBytes exercises the production reader.
// Closed status/pin/digest distinctions never copy a private response body or credential.
// TestArtifactInitializationReasonsFollowActualHTTPAndBytes проверяет production reader;
// closed status/pin/digest различия не копируют private response body либо credential.
func TestArtifactInitializationReasonsFollowActualHTTPAndBytes(t *testing.T) {
	content := []byte("input")
	hash := sha256.Sum256(content)
	artifact := runtimecontract.RunnerInputArtifact{Ref: "artifact_fixture", MediaType: "text/plain", SizeBytes: int64(len(content)), Digest: "sha256:" + hex.EncodeToString(hash[:])}
	for _, item := range []struct {
		name           string
		status         int
		metadata, body string
		reason         string
	}{
		{"401", 401, "", "PRIVATE_SENTINEL", "AUTHENTICATION_REJECTED"}, {"403", 403, "", "PRIVATE_SENTINEL", "AUTHORITY_REJECTED"},
		{"404", 404, "", "PRIVATE_SENTINEL", "SOURCE_UNAVAILABLE"}, {"503", 503, "", "PRIVATE_SENTINEL", "UPSTREAM_UNAVAILABLE"},
		{"metadata", 200, "bad", string(content), "PIN_MISMATCH"}, {"digest", 200, artifact.Digest, "wrong", "CONTENT_DIGEST_MISMATCH"},
		{"success", 200, artifact.Digest, string(content), ""},
	} {
		t.Run(item.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				w.Header().Set("X-Kodex-Artifact-Digest", item.metadata)
				w.Header().Set("Content-Length", "5")
				w.WriteHeader(item.status)
				_, _ = io.WriteString(w, item.body)
			}))
			defer upstream.Close()
			base, _ := url.Parse(upstream.URL)
			client := &Client{files: upstream.Client(), base: base}
			err := client.WriteArtifact(t.Context(), runtimecontract.RunnerInput{LeaseRef: "lease_fixture"}, artifact, io.Discard)
			if item.reason == "" {
				if err != nil {
					t.Fatal("valid input rejected")
				}
				return
			}
			stage, reason := runtimecontract.InitializationErrorDetails(err)
			if err == nil || stage != "ARTIFACT_TRANSFER" || reason != item.reason || strings.Contains(err.Error(), "PRIVATE_SENTINEL") {
				t.Fatal("HTTP evidence collapsed or raw body leaked")
			}
		})
	}
}
