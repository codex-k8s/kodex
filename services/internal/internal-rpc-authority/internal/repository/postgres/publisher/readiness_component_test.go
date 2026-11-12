package publisher

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/repository/postgres/session"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPublisherReadinessPrivilegesComponent checks each required receipt privilege using
// the migrated disposable database and exact publisher LOGIN/capability boundary. It restores
// revoked grants before returning and never rotates identities or publishes material.
// TestPublisherReadinessPrivilegesComponent проверяет каждое право таблицы квитанций в
// мигрированной disposable БД с точной границей LOGIN/capability publisher. Отозванные права
// восстанавливаются до возврата; идентичности не ротируются, материал не публикуется.
func TestPublisherReadinessPrivilegesComponent(t *testing.T) {
	dsn := os.Getenv("KODEX_AUTHORITY_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("disposable PostgreSQL DSN is not configured")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil || config.ConnConfig.User != "postgres" || config.ConnConfig.Database != "internal_rpc_authority" ||
		(config.ConnConfig.Host != "127.0.0.1" && config.ConnConfig.Host != "localhost") || config.ConnConfig.Port < 1024 {
		t.Fatal("exact loopback disposable administrator is required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	admin, err := pgx.ConnectConfig(ctx, config.ConnConfig.Copy())
	if err != nil {
		t.Fatal("connect disposable publisher administrator")
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer stop()
		_ = admin.Close(cleanup)
	}()
	config.ConnConfig.User = "ira_publisher_g4"
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		return session.Configure(ctx, conn, "ira_publisher_g4", session.CapabilityPublisher)
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("open disposable publisher pool")
	}
	defer pool.Close()
	r := &Repository{pool: pool}
	if err := r.PublisherReady(ctx); err != nil {
		t.Fatal("complete publisher privileges did not admit readiness")
	}
	for _, privilege := range []string{"SELECT", "INSERT"} {
		t.Run(privilege, func(t *testing.T) {
			if _, err := admin.Exec(ctx, "REVOKE "+privilege+" ON internal_rpc_authority.authority_publisher_delivery_receipts FROM internal_rpc_authority_publisher"); err != nil {
				t.Fatal("revoke isolated publisher receipt privilege")
			}
			defer func() {
				cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
				defer stop()
				if _, err := admin.Exec(cleanup, "GRANT "+privilege+" ON internal_rpc_authority.authority_publisher_delivery_receipts TO internal_rpc_authority_publisher"); err != nil {
					t.Error("restore isolated publisher receipt privilege")
				}
			}()
			if err := r.PublisherReady(ctx); err == nil {
				t.Fatal("publisher readiness admitted a missing receipt privilege")
			}
		})
		if err := r.PublisherReady(ctx); err != nil {
			t.Fatal("publisher readiness did not recover after exact grant restoration")
		}
	}
}
