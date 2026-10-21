package retention

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/s3store"
)

// TestSeaweedFSExactVersionAbsentE2E reads one explicitly named version from a
// loopback-only local bucket and requires an authoritative not-found response.
// RU: TestSeaweedFSExactVersionAbsentE2E читает явно заданную версию из локального
// bucket только на loopback и требует авторитетный ответ об отсутствии.
func TestSeaweedFSExactVersionAbsentE2E(t *testing.T) {
	if os.Getenv("ARTIFACT_STORAGE_E2E") != "1" {
		t.Skip("local artifact storage E2E is disabled")
	}
	endpoint := objectstoragetest.RequireLoopbackHTTPEndpoint(t, os.Getenv("ARTIFACT_STORAGE_E2E_ENDPOINT"), "artifact E2E")
	accessKey := objectstoragetest.ReadE2ECredential(t, os.Getenv("ARTIFACT_STORAGE_E2E_ACCESS_KEY_FILE"), "artifact E2E")
	secretKey := objectstoragetest.ReadE2ECredential(t, os.Getenv("ARTIFACT_STORAGE_E2E_SECRET_KEY_FILE"), "artifact E2E")
	key := strings.TrimSpace(os.Getenv("ARTIFACT_STORAGE_E2E_OBJECT_KEY"))
	version := strings.TrimSpace(os.Getenv("ARTIFACT_STORAGE_E2E_OBJECT_VERSION"))
	if !objectstorage.ValidKey(key) || version == "" || strings.ContainsAny(version, "\r\n") {
		t.Fatal("artifact object locator is invalid")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := s3store.New(ctx, s3store.Config{
		Endpoint: endpoint, Region: "us-east-1", Bucket: "kodex-artifacts",
		AccessKeyID: accessKey, SecretKey: secretKey, UsePathStyle: true,
	})
	if err != nil || store.Check(ctx) != nil {
		t.Fatal("local SeaweedFS artifact bucket is unavailable")
	}
	if _, err := store.Head(ctx, key, version); !errors.Is(err, objectstorage.ErrNotFound) {
		t.Fatalf("artifact exact object version remains authoritative: %v", err)
	}
}
