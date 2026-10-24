package s3backup

import (
	"context"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/configspec"
	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/manifest"
)

// TestRestoreUsesScopedPrefix exercises the production SDK request against a TLS endpoint that denies listings outside the exact restore directory.
// RU: TestRestoreUsesScopedPrefix проверяет настоящий запрос SDK через TLS: сервер запрещает перечисление за пределами точного каталога восстановления.
func TestRestoreUsesScopedPrefix(t *testing.T) {
	for _, prefix := range []string{"e2e-restore-fixture", "e2e-restore-fixture/"} {
		t.Run(prefix, func(t *testing.T) {
			var listed atomic.Bool
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/restore-fixture" && r.URL.Path != "/restore-fixture/" {
					http.Error(w, "unexpected bucket", http.StatusForbidden)
					return
				}
				if r.Method == http.MethodHead {
					w.WriteHeader(http.StatusOK)
					return
				}
				w.Header().Set("Content-Type", "application/xml")
				switch {
				case r.Method == http.MethodGet && r.URL.Query().Has("versioning"):
					fmt.Fprint(w, `<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>Enabled</Status></VersioningConfiguration>`)
				case r.Method == http.MethodGet && r.URL.Query().Has("versions"):
					if r.URL.Query().Get("prefix") != "e2e-restore-fixture/" {
						w.WriteHeader(http.StatusForbidden)
						fmt.Fprint(w, `<Error><Code>AccessDenied</Code></Error>`)
						return
					}
					listed.Store(true)
					fmt.Fprint(w, `<ListVersionsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><IsTruncated>false</IsTruncated></ListVersionsResult>`)
				default:
					http.Error(w, "unexpected operation", http.StatusForbidden)
				}
			}))
			defer server.Close()
			caPath := filepath.Join(t.TempDir(), "ca.pem")
			if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("AWS_CA_BUNDLE", caPath)
			repository, _ := testRepository(t)
			target := configspec.S3{Endpoint: server.URL, Region: "us-east-1", Bucket: "restore-fixture", Prefix: prefix,
				AccessKeyID: "fixture", SecretAccessKey: "fixture", UsePathStyle: true}
			if _, err := repository.RestorePlatformObjects(context.Background(), manifest.Manifest{}, target, t.TempDir()); err != nil {
				t.Fatalf("restore scoped prefix: %v", err)
			}
			if !listed.Load() {
				t.Fatal("restore did not check the exact target prefix")
			}
		})
	}
}
