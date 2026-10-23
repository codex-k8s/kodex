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

	model "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
	authoritysnapshot "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/snapshot"
)

// roundTripFunc adapts an in-memory callback to HTTP transport so delivery units exercise request/response code without a network server or Kubernetes credentials.
// roundTripFunc адаптирует callback в памяти к HTTP transport, чтобы unit доставки выполняли код запроса и ответа без сетевого сервера и Kubernetes credentials.
type roundTripFunc func(*http.Request) (*http.Response, error)

// RoundTrip delegates the exact request to the fixture callback and returns its response/error unchanged; TLS, authentication and a real API are not exercised.
// RoundTrip передаёт точный запрос callback эталона и возвращает ответ и ошибку без изменения; TLS, аутентификация и реальный API не выполняются.
func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

// TestPublishAcceptsFullPublisherSnapshot sends a synthetic nine-KiB compact value through fake GET/PUT responses and checks exact bytes, returned resource version and two calls. It proves adapter size handling, not valid JWS signing or live Secret CAS.
// TestPublishAcceptsFullPublisherSnapshot проводит синтетическое девяти-КиБ compact-значение через фиктивные GET и PUT, проверяя байты, возвращённую resource version и два вызова. Доказывается обработка размера адаптером, а не корректная подпись JWS и реальный CAS Secret.
func TestPublishAcceptsFullPublisherSnapshot(t *testing.T) {
	t.Parallel()

	compact := strings.Repeat("a", 9<<10)
	digest := strings.Repeat("b", 64)
	requests := 0
	tokenFile := writeTokenFile(t)
	delivery := &Delivery{
		config: Config{
			Namespace:  "kodex-system",
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

// TestReadAcceptsFullPublisherSnapshot returns a synthetic nine-KiB Base64 value from a fake GET and checks it survives the read path beyond the former eight-KiB bound. No real signature, token authorization or Kubernetes API is involved.
// TestReadAcceptsFullPublisherSnapshot возвращает синтетическое девяти-КиБ Base64-значение фиктивным GET и проверяет путь чтения сверх прежней границы восемь КиБ. Реальные подпись, авторизация токена и Kubernetes API не используются.
func TestReadAcceptsFullPublisherSnapshot(t *testing.T) {
	t.Parallel()

	compact := strings.Repeat("a", 9<<10)
	envelope := validEnvelope()
	envelope.Metadata.Annotations = map[string]string{
		"kodex.dev/source-revision":      "1",
		"kodex.dev/source-digest-sha256": strings.Repeat("b", 64),
	}
	envelope.Data = map[string]string{
		"snapshot.jws": base64.StdEncoding.EncodeToString([]byte(compact)),
	}
	tokenFile := writeTokenFile(t)
	delivery := &Delivery{
		config: Config{
			Namespace:  "kodex-system",
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

// writeTokenFile creates a fixed synthetic token in the test-owned temporary directory with mode 0600 and fails the unit on write error; no real credential is read or copied.
// writeTokenFile создаёт фиксированный синтетический токен в принадлежащем тесту временном каталоге с mode 0600 и останавливает unit при ошибке записи; реальный credential не читается и не копируется.
func writeTokenFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("test-token"), 0o600); err != nil {
		t.Fatalf("write test token: %v", err)
	}
	return path
}

// validEnvelope constructs fresh maps for a synthetic Opaque Secret with the adapter's fixed name/namespace and nonempty UID/resourceVersion; snapshot annotations and bytes are filled by each unit.
// validEnvelope создаёт новые карты синтетического Opaque Secret с фиксированными именем и namespace адаптера и непустыми UID и resourceVersion; annotations и байты снимка задаёт каждый unit.
func validEnvelope() secretEnvelope {
	envelope := secretEnvelope{
		APIVersion: "v1",
		Kind:       "Secret",
		Type:       "Opaque",
		Data:       map[string]string{},
	}
	envelope.Metadata.Name = "internal-rpc-authority-snapshot"
	envelope.Metadata.Namespace = "kodex-system"
	envelope.Metadata.UID = "snapshot-uid"
	envelope.Metadata.ResourceVersion = "1"
	envelope.Metadata.Annotations = map[string]string{}
	return envelope
}

// jsonResponse JSON-encodes the supplied fixture and wraps it in an HTTP 200 response with a closable in-memory body, failing the unit on encoding error.
// jsonResponse кодирует переданный эталон в JSON и оборачивает его HTTP-ответом 200 с закрываемым телом в памяти, останавливая unit при ошибке кодирования.
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
