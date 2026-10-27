package callback

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

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
