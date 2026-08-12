package snapshot

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	model "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
	authoritysnapshot "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/snapshot"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestPublishAcceptsFullPublisherSnapshot(t *testing.T) {
	t.Parallel()

	compact := strings.Repeat("a", 9<<10)
	digest := strings.Repeat("b", 64)
	requests := 0
	tokenFile := writeTokenFile(t)
	delivery := &Delivery{
		config: Config{
			Namespace:  "mattercodex-system",
			SecretName: "internal-rpc-authority-snapshot",
			TokenFile:  tokenFile,
		},
		resourceURL: "https://kubernetes.invalid/snapshot",
		client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			requests++
			var envelope secretEnvelope
			switch request.Method {
			case http.MethodGet:
				envelope = validEnvelope()
			case http.MethodPut:
				if err := json.NewDecoder(request.Body).Decode(&envelope); err != nil {
					t.Fatalf("decode publication request: %v", err)
				}
				envelope.Metadata.ResourceVersion = "2"
			default:
				t.Fatalf("unexpected method %s", request.Method)
			}
			return jsonResponse(t, envelope), nil
		})},
	}

	publication, err := delivery.Publish(context.Background(), model.AuthoritySnapshotPublication{
		SourceRevision:     1,
		SourceDigestSHA256: digest,
		SnapshotCompactJWS: compact,
		SignerGeneration:   1,
	})
	if err != nil {
		t.Fatalf("publish full snapshot: %v", err)
	}
	if publication.SnapshotCompactJWS != compact || publication.SnapshotResourceVersion != "2" {
		t.Fatal("published snapshot readback mismatch")
	}
	if requests != 2 {
		t.Fatalf("unexpected request count: got %d, want 2", requests)
	}
}

func TestReadAcceptsFullPublisherSnapshot(t *testing.T) {
	t.Parallel()

	compact := strings.Repeat("a", 9<<10)
	envelope := validEnvelope()
	envelope.Metadata.Annotations = map[string]string{
		"mattercodex.dev/source-revision":      "1",
		"mattercodex.dev/source-digest-sha256": strings.Repeat("b", 64),
	}
	envelope.Data = map[string]string{
		"snapshot.jws": base64.StdEncoding.EncodeToString([]byte(compact)),
	}
	tokenFile := writeTokenFile(t)
	delivery := &Delivery{
		config: Config{
			Namespace:  "mattercodex-system",
			SecretName: "internal-rpc-authority-snapshot",
			TokenFile:  tokenFile,
		},
		resourceURL: "https://kubernetes.invalid/snapshot",
		client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.Method != http.MethodGet {
				t.Fatalf("unexpected method %s", request.Method)
			}
			return jsonResponse(t, envelope), nil
		})},
	}

	publication, err := delivery.Read(context.Background())
	if err != nil {
		t.Fatalf("read full snapshot: %v", err)
	}
	if publication.SnapshotCompactJWS != compact {
		t.Fatal("served snapshot readback mismatch")
	}
	if len(publication.SnapshotCompactJWS) <= 8192 ||
		len(publication.SnapshotCompactJWS) > authoritysnapshot.MaxPublisherSnapshotBytes {
		t.Fatal("test fixture does not cover the publisher snapshot limit")
	}
}

func writeTokenFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("test-token"), 0o600); err != nil {
		t.Fatalf("write test token: %v", err)
	}
	return path
}

func validEnvelope() secretEnvelope {
	envelope := secretEnvelope{
		APIVersion: "v1",
		Kind:       "Secret",
		Type:       "Opaque",
		Data:       map[string]string{},
	}
	envelope.Metadata.Name = "internal-rpc-authority-snapshot"
	envelope.Metadata.Namespace = "mattercodex-system"
	envelope.Metadata.UID = "snapshot-uid"
	envelope.Metadata.ResourceVersion = "1"
	envelope.Metadata.Annotations = map[string]string{}
	return envelope
}

func jsonResponse(t *testing.T, value any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode response: %v", err)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(string(raw))),
		Header:     make(http.Header),
	}
}
