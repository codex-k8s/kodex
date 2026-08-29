package retention

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/s3store"
)

func TestSeaweedFSExactVersionAbsentE2E(t *testing.T) {
	if os.Getenv("ARTIFACT_STORAGE_E2E") != "1" {
		t.Skip("local artifact storage E2E is disabled")
	}
	endpoint := requireArtifactLoopbackEndpoint(t, os.Getenv("ARTIFACT_STORAGE_E2E_ENDPOINT"))
	accessKey := readArtifactE2ESecret(t, os.Getenv("ARTIFACT_STORAGE_E2E_ACCESS_KEY_FILE"))
	secretKey := readArtifactE2ESecret(t, os.Getenv("ARTIFACT_STORAGE_E2E_SECRET_KEY_FILE"))
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

func requireArtifactLoopbackEndpoint(t *testing.T, raw string) string {
	t.Helper()
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Scheme != "http" || endpoint.User != nil || endpoint.RawQuery != "" ||
		endpoint.Fragment != "" || (endpoint.Path != "" && endpoint.Path != "/") ||
		(endpoint.Hostname() != "127.0.0.1" && endpoint.Hostname() != "localhost") || endpoint.Port() == "" {
		t.Fatal("artifact E2E endpoint must be loopback-only HTTP")
	}
	return raw
}

func readArtifactE2ESecret(t *testing.T, path string) string {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		!filepath.IsAbs(path) || info.Mode().Perm()&0o077 != 0 {
		t.Fatal("artifact E2E credential file is invalid")
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || len(raw) > 16<<10 {
		t.Fatal("artifact E2E credential cannot be read")
	}
	value := strings.TrimSpace(string(raw))
	for index := range raw {
		raw[index] = 0
	}
	if value == "" || strings.ContainsAny(value, "\r\n") {
		t.Fatal(errors.New("artifact E2E credential is invalid"))
	}
	return value
}
