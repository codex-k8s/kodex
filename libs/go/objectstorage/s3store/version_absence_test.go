package s3store

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
)

// TestGenericS3HeadDoesNotProveVersionAbsence exercises actual TLS404/403 without widening S3 scope.
// Ordinary HEAD mapping stays compatible, but it never becomes a qualified exact deletion proof.
// RU: TestGenericS3HeadDoesNotProveVersionAbsence проверяет actual TLS404/403 без расширения S3 scope.
// Обычный HEAD сохраняет mapping, но не становится qualified exact deletion proof.
func TestGenericS3HeadDoesNotProveVersionAbsence(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusForbidden} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			var calls atomic.Int64
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodHead || r.URL.Query().Get("versionId") != "pinned-version" || r.URL.Path != "/synthetic/key" || r.Header.Get("Authorization") == "" {
					t.Error("unexpected exact signed HEAD")
				}
				w.WriteHeader(status)
			}))
			defer server.Close()
			client := server.Client()
			defer client.CloseIdleConnections()
			store, err := New(t.Context(), Config{Endpoint: server.URL, Region: "us-east-1", Bucket: "synthetic", AccessKeyID: "synthetic-key", SecretKey: "synthetic-secret", UsePathStyle: true, HTTPClient: client})
			if err != nil {
				t.Fatal(err)
			}
			_, headErr := store.Head(t.Context(), "key", "pinned-version")
			if headErr == nil || status == http.StatusNotFound && !errors.Is(headErr, objectstorage.ErrNotFound) || status == http.StatusForbidden && errors.Is(headErr, objectstorage.ErrNotFound) {
				t.Fatal("ordinary HEAD semantics changed", headErr)
			}
			if err := objectstorage.VerifyVersionAbsent(t.Context(), store, "key", "pinned-version"); !errors.Is(err, objectstorage.ErrUnavailable) || calls.Load() != 1 {
				t.Fatal("generic HEAD became absence proof or issued extra IO", err)
			}
		})
	}
}
