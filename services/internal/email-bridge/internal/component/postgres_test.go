package component

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"
	"time"

	repository "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/repository/postgres/receipt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func postgresFixture(t *testing.T) *repository.Repository {
	t.Helper()
	runtimeDSN := os.Getenv("EMAIL_BRIDGE_TEST_DSN")
	adminDSN := os.Getenv("EMAIL_BRIDGE_TEST_ADMIN_DSN")
	if runtimeDSN == "" {
		t.Skip("disposable PostgreSQL not configured")
	}
	runtimeURL, err := url.Parse(runtimeDSN)
	adminURL, adminErr := url.Parse(adminDSN)
	if err != nil || adminErr != nil || runtimeURL.Hostname() != "127.0.0.1" || adminURL.Host != runtimeURL.Host || runtimeURL.User == nil || runtimeURL.User.Username() != "email_bridge_runtime" || adminURL.User == nil || adminURL.User.Username() != "postgres" || adminURL.Path != "/postgres" || runtimeURL.Path != "/email_bridge" {
		t.Fatal("unsafe disposable PostgreSQL fixture")
	}
	base := t.Context()
	admin, err := pgxpool.New(base, adminDSN)
	if err != nil {
		t.Fatal("fixture administrator unavailable")
	}
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		admin.Close()
		t.Fatal("fixture identity unavailable")
	}
	name := "email_fixture_" + hex.EncodeToString(random[:])
	quoted := pgx.Identifier{name}.Sanitize()
	ctx, cancel := context.WithTimeout(base, 10*time.Second)
	defer cancel()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+quoted+" TEMPLATE email_bridge OWNER email_bridge_migrator"); err != nil {
		admin.Close()
		t.Fatal("fixture database creation failed")
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(base), 5*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanup, "DROP DATABASE "+quoted+" WITH (FORCE)"); err != nil {
			t.Error("fixture database cleanup failed")
		}
		admin.Close()
	})
	runtimeURL.Path = "/" + name
	pool, err := pgxpool.New(ctx, runtimeURL.String())
	if err != nil {
		t.Fatal("fixture runtime unavailable")
	}
	t.Cleanup(pool.Close)
	store := &repository.Repository{Pool: pool}
	if err := store.Ready(ctx); err != nil {
		t.Fatal("fixture schema unavailable")
	}
	return store
}
