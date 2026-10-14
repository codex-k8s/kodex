package postgres

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/services/jobs/artifact-retention/internal/retention"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed testdata/revision_retention_seed.sql
var revisionRetentionSeed string

//go:embed testdata/revision_retention_readback.sql
var revisionRetentionReadback string

func TestRevisionRetentionComponent(t *testing.T) {
	dsn := os.Getenv("KODEX_ARTIFACT_RETENTION_COMPONENT_DSN")
	if dsn == "" {
		t.Skip("dedicated disposable retention database is required")
	}
	runtimeConfig, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid disposable runtime configuration")
	}
	adminConfig, err := pgx.ParseConfig(os.Getenv("KODEX_ARTIFACT_RETENTION_COMPONENT_ADMIN_DSN"))
	if err != nil || runtimeConfig.Host != "127.0.0.1" || runtimeConfig.Port < 1024 || runtimeConfig.Database != "control_plane" || runtimeConfig.User != "artifact_retention_runtime_g1" || runtimeConfig.Password != "" || runtimeConfig.TLSConfig != nil || len(runtimeConfig.Fallbacks) != 0 || adminConfig.Host != runtimeConfig.Host || adminConfig.Port != runtimeConfig.Port || adminConfig.Database != runtimeConfig.Database || adminConfig.User != "postgres" || adminConfig.Password != "" || adminConfig.TLSConfig != nil || len(adminConfig.Fallbacks) != 0 || os.Getenv("KODEX_ARTIFACT_REVISION_DISPOSABLE") != "dedicated-loopback-container" {
		t.Fatal("retention fixture must identify the dedicated loopback container")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	admin, err := pgx.ConnectConfig(ctx, adminConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	objects := objectstoragetest.New()
	receipts := make([]objectstorage.Receipt, 0, 2)
	for _, body := range []string{"old", "new"} {
		sum := sha256.Sum256([]byte(body))
		receipt, err := objects.Put(ctx, objectstorage.PutInput{Key: "disposable/retention/" + body, MediaType: "text/plain", Digest: "sha256:" + hex.EncodeToString(sum[:]), SizeBytes: 3, Body: strings.NewReader(body)})
		if err != nil {
			t.Fatal(err)
		}
		receipts = append(receipts, receipt)
	}
	if _, err := admin.Exec(ctx, revisionRetentionSeed, pgx.QueryExecModeSimpleProtocol, receipts[0].Key, receipts[0].VersionID, receipts[0].Digest, receipts[1].Key, receipts[1].VersionID, receipts[1].Digest); err != nil {
		t.Fatalf("seed immutable retention: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository := New(pool)
	claims, err := repository.Claim(ctx, "retention-first", 1, 60)
	if err != nil || len(claims) != 1 || claims[0].ObjectVersion != receipts[0].VersionID {
		t.Fatalf("first exact revision claim: count=%d err=%v", len(claims), err)
	}
	wrong := claims[0]
	wrong.ObjectVersion = "substituted-exact-version"
	if err := repository.Finalize(ctx, wrong, "retention-first"); !errors.Is(err, retention.ErrLostClaim) {
		t.Fatalf("wrong receipt finalized: %v", err)
	}
	if err := repository.Finalize(ctx, claims[0], "wrong-worker"); !errors.Is(err, retention.ErrLostClaim) {
		t.Fatalf("foreign worker finalized: %v", err)
	}
	if err := objects.Delete(ctx, claims[0].ObjectKey, claims[0].ObjectVersion); err != nil {
		t.Fatal(err)
	}
	if _, err := objects.Head(ctx, claims[0].ObjectKey, claims[0].ObjectVersion); !errors.Is(err, objectstorage.ErrNotFound) {
		t.Fatal("first exact deletion readback")
	}
	if err := repository.Finalize(ctx, claims[0], "retention-first"); err != nil {
		t.Fatalf("close first revision: %v", err)
	}
	assertRevisionRetentionState(t, ctx, admin, false, "PURGE_PENDING", 1, 2)
	if err := repository.Finalize(ctx, claims[0], "retention-first"); !errors.Is(err, retention.ErrLostClaim) {
		t.Fatalf("stale generation finalized: %v", err)
	}
	processor := retention.NewProcessor(repository, objects)
	if processed, err := processor.Process(ctx, "retention-second", 1, 60); err != nil || processed != 1 {
		t.Fatalf("close second revision: %d/%v", processed, err)
	}
	assertRevisionRetentionState(t, ctx, admin, true, "PURGED", 0, 0)
	if _, err := objects.Head(ctx, receipts[1].Key, receipts[1].VersionID); !errors.Is(err, objectstorage.ErrNotFound) {
		t.Fatal("second exact object survived")
	}
	if processed, err := processor.Process(ctx, "retention-terminal", 1, 60); err != nil || processed != 0 {
		t.Fatalf("terminal retention retry: %d/%v", processed, err)
	}
}

func assertRevisionRetentionState(t *testing.T, ctx context.Context, admin *pgx.Conn, terminal bool, wantState string, wantContent, wantRevisions int64) {
	t.Helper()
	var state string
	var pointerNull bool
	var content, revisions, bindings, grants int64
	if err := admin.QueryRow(ctx, revisionRetentionReadback, terminal).Scan(&state, &pointerNull, &content, &revisions, &bindings, &grants); err != nil {
		t.Fatal(err)
	}
	if state != wantState || pointerNull != terminal || content != wantContent || revisions != wantRevisions || bindings != 1 || grants != 1 {
		t.Fatalf("retention state: %s null=%v content=%d revisions=%d bindings=%d grants=%d", state, pointerNull, content, revisions, bindings, grants)
	}
}
