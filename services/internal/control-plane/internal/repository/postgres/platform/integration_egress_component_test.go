package platform

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	shared "github.com/codex-k8s/kodex/libs/go/integrationegresspolicy"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/jackc/pgx/v5/pgxpool"
)

type noOriginResolver struct{}

func (noOriginResolver) Resolve(context.Context, string) (shared.Snapshot, error) {
	return shared.Snapshot{}, errors.New("empty first-run projection must not resolve DNS")
}

func TestIntegrationEgressProjectionComponent(t *testing.T) {
	dsn := os.Getenv("KODEX_CONTROL_PLANE_TEST_DSN")
	if dsn == "" {
		t.Skip("KODEX_CONTROL_PLANE_TEST_DSN is not configured")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository, err := New(pool, "openai-codex", "gpt-6-sol", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	hosts, err := repository.IntegrationEgressHostnames(ctx)
	if err != nil || len(hosts) != 0 {
		t.Fatalf("first-run origins = %v: %v", hosts, err)
	}
	baseDigest := strings.Repeat("a", 64)
	first, err := repository.PrepareIntegrationEgressProjection(ctx, baseDigest, noOriginResolver{})
	if err != nil || first.Generation != 1 || len(first.Destinations) != 0 || first.Validate() != nil {
		t.Fatalf("first-run document = %#v: %v", first, err)
	}
	second, err := repository.PrepareIntegrationEgressProjection(ctx, baseDigest, noOriginResolver{})
	if err != nil || second.Digest() != first.Digest() || second.Generation != first.Generation {
		t.Fatalf("identical replay advanced generation: %#v: %v", second, err)
	}
	var generation int64
	var digest string
	var storedRaw []byte
	if err := pool.QueryRow(ctx, `SELECT generation,target_digest,document FROM control_plane.integration_egress_projection WHERE singleton=true`).Scan(&generation, &digest, &storedRaw); err != nil || generation != 1 || digest != first.Digest() || len(storedRaw) == 0 {
		t.Fatalf("durable projection mismatch: generation=%d digest=%q err=%v", generation, digest, err)
	}
}
