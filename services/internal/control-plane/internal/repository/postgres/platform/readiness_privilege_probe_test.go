package platform

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// newReadinessPrivilegeProbe opens a separate nonowner session in an already validated disposable
// database. Its random LOGIN role has no inherited application rights. Cleanup removes only that
// role's grants before database cleanup; synthetic authentication never uses live credentials.
// newReadinessPrivilegeProbe открывает отдельную сессию без прав владельца в проверенной одноразовой
// БД. Случайная LOGIN-роль не наследует права приложения. Очистка снимает только её grants перед
// удалением БД; синтетическая авторизация не использует credentials живого окружения.
func newReadinessPrivilegeProbe(t *testing.T, ctx context.Context, application *pgx.ConnConfig) (*pgx.Conn, *pgx.Conn) {
	t.Helper()
	adminConfig, err := pgx.ParseConfig(os.Getenv("KODEX_CONTROL_PLANE_TEST_ADMIN_DSN"))
	if err != nil || application == nil || adminConfig.Database != "postgres" || adminConfig.User != "postgres" ||
		adminConfig.Host != application.Host || adminConfig.Port != application.Port || adminConfig.Port < 1024 ||
		(adminConfig.Host != "localhost" && adminConfig.Host != "127.0.0.1") ||
		!strings.HasPrefix(application.Database, "kodex_assistant_test_") {
		t.Fatal("readiness administrator does not match the disposable fixture")
	}
	adminConfig.Database = application.Database
	admin, err := pgx.ConnectConfig(ctx, adminConfig)
	if err != nil {
		t.Fatal("connect disposable readiness administrator")
	}
	role := "kodex_readiness_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	identifier := pgx.Identifier{role}.Sanitize()
	created := false
	var probe *pgx.Conn
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if probe != nil {
			if err := probe.Close(cleanup); err != nil {
				t.Error("close disposable readiness probe")
			}
		}
		if created {
			if _, err := admin.Exec(cleanup, "DROP OWNED BY "+identifier); err != nil {
				t.Error("remove exact disposable readiness grants")
			}
			if _, err := admin.Exec(cleanup, "DROP ROLE "+identifier); err != nil {
				t.Error("remove exact disposable readiness role")
			}
			var remains bool
			if err := admin.QueryRow(cleanup, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)", role).Scan(&remains); err != nil || remains {
				t.Error("disposable readiness role absence is not confirmed")
			}
		}
		if err := admin.Close(cleanup); err != nil {
			t.Error("close disposable readiness administrator")
		}
	})
	const syntheticPassword = "synthetic-readiness-only"
	if _, err := admin.Exec(ctx, "CREATE ROLE "+identifier+" LOGIN NOINHERIT PASSWORD '"+syntheticPassword+"'"); err != nil {
		t.Fatal("create disposable readiness role")
	}
	created = true
	if _, err := admin.Exec(ctx, "GRANT USAGE ON SCHEMA control_plane TO "+identifier); err != nil {
		t.Fatal("grant disposable readiness schema visibility")
	}
	probeConfig := application.Copy()
	probeConfig.User, probeConfig.Password = role, syntheticPassword
	probe, err = pgx.ConnectConfig(ctx, probeConfig)
	if err != nil {
		t.Fatal("connect separate readiness session")
	}
	var session, current string
	var privileged bool
	if probe.QueryRow(ctx, "SELECT session_user,current_user,rolsuper OR rolinherit OR rolcreaterole OR rolcreatedb OR rolbypassrls FROM pg_roles WHERE rolname=session_user").Scan(&session, &current, &privileged) != nil || session != role || current != role || privileged {
		t.Fatal("readiness probe did not retain an unprivileged session identity")
	}
	return admin, probe
}
