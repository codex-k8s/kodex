package component

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	repository "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/repository/postgres/receipt"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPostgresReadinessKeepsCauseAndRejectsWrongRole uses one disposable schema to distinguish a false role assertion from a typed SQL failure.
// TestPostgresReadinessKeepsCauseAndRejectsWrongRole различает ложную проверку роли и типизированный SQL-отказ в одной disposable-схеме.
func TestPostgresReadinessKeepsCauseAndRejectsWrongRole(t *testing.T) {
	store := postgresFixture(t)
	config := store.Pool.Config().Copy()
	config.ConnConfig.User = "postgres"
	admin, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal("disposable administrator pool unavailable")
	}
	defer admin.Close()
	wrong := &repository.Repository{Pool: admin}
	if err := wrong.Ready(t.Context()); err != errs.Unavailable {
		t.Fatal("wrong runtime role admitted or assertion identity lost")
	}
	for _, table := range []string{"receipts", "owner_receipts"} {
		for _, privilege := range []string{"SELECT", "INSERT", "UPDATE"} {
			t.Run(table+"/"+privilege, func(t *testing.T) {
				if _, err := admin.Exec(t.Context(), "REVOKE "+privilege+" ON email_bridge."+table+" FROM email_bridge_runtime"); err != nil {
					t.Fatal("disposable privilege fixture unavailable")
				}
				if err := store.Ready(t.Context()); !errors.Is(err, errs.Unavailable) {
					t.Fatal("partially privileged runtime admitted")
				}
				if _, err := admin.Exec(t.Context(), "GRANT "+privilege+" ON email_bridge."+table+" TO email_bridge_runtime"); err != nil {
					t.Fatal("disposable privilege restoration unavailable")
				}
				if err := store.Ready(t.Context()); err != nil {
					t.Fatal("restored exact runtime rejected")
				}
			})
		}
	}
	if _, err := admin.Exec(t.Context(), "DROP TABLE email_bridge.owner_receipts CASCADE"); err != nil {
		t.Fatal("disposable missing schema fixture unavailable")
	}
	err = store.Ready(t.Context())
	var pg *pgconn.PgError
	if !errors.Is(err, errs.Unavailable) || !errors.As(err, &pg) || pg.Code != "42P01" || err.Error() != "UNAVAILABLE" {
		t.Fatal("typed schema error or safe domain identity lost")
	}
}
