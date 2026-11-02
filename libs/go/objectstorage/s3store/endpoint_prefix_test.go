package s3store_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/s3store"
)

// TestHTTPSPrefixReachesSignedSDKRequest checks the actual adapter constructor and TLS request, preserving the selected mount and bucket.
// RU: TestHTTPSPrefixReachesSignedSDKRequest проверяет настоящий constructor и TLS-запрос адаптера с сохранением выбранного mount и bucket.
func TestHTTPSPrefixReachesSignedSDKRequest(t *testing.T) {
	t.Parallel()
	observed := make(chan string, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead || r.URL.EscapedPath() != "/services/s3/synthetic-bucket" ||
			!strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=synthetic-access/") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		observed <- r.URL.EscapedPath()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	store, err := s3store.New(t.Context(), s3store.Config{
		Endpoint: server.URL + "/services/s3", Region: "us-east-1", Bucket: "synthetic-bucket",
		AccessKeyID: "synthetic-access", SecretKey: "synthetic-secret-key", UsePathStyle: true,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal("construct path-style storage adapter")
	}
	if err := store.Check(t.Context()); err != nil {
		t.Fatal("check prefixed bucket over the pinned test CA")
	}
	select {
	case <-observed:
	default:
		t.Fatal("no signed request reached the configured mount")
	}
}

// TestHTTPSPrefixRejectsVirtualHostedMode closes configuration before an SDK request could change the chosen authority.
// RU: TestHTTPSPrefixRejectsVirtualHostedMode закрывает конфигурацию до SDK-запроса, способного изменить выбранный authority.
func TestHTTPSPrefixRejectsVirtualHostedMode(t *testing.T) {
	t.Parallel()
	store, err := s3store.New(t.Context(), s3store.Config{
		Endpoint: "https://s3.example.invalid/s3", Region: "us-east-1", Bucket: "synthetic-bucket",
		AccessKeyID: "synthetic-access", SecretKey: "synthetic-secret-key",
	})
	if store != nil || !errors.Is(err, objectstorage.ErrInvalid) {
		t.Fatal("non-path-style prefix was accepted")
	}
}
