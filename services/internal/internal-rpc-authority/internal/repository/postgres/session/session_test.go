package session

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/failure"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestConfigureDeniedCapabilityPreservesSQLState uses a disposable issuer LOGIN to prove a forbidden SET ROLE remains denied and diagnosable.
// TestConfigureDeniedCapabilityPreservesSQLState использует disposable LOGIN issuer и доказывает сохранение отказа и SQLSTATE запрещённого SET ROLE.
func TestConfigureDeniedCapabilityPreservesSQLState(t *testing.T) {
	dsn := os.Getenv("KODEX_AUTHORITY_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("disposable PostgreSQL DSN is not configured")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("parse disposable PostgreSQL configuration")
	}
	config.User = "ira_control_api_gateway_issuer_g1"
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	connection, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatal("connect disposable issuer LOGIN")
	}
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second)
		defer closeCancel()
		_ = connection.Close(closeCtx)
	})
	for _, configure := range []func(context.Context, *pgx.Conn, string, string) error{Configure, Ensure} {
		err := configure(ctx, connection, config.User, CapabilityPublisher)
		var postgres *pgconn.PgError
		if !errors.As(err, &postgres) || postgres.Code != "42501" || !failure.IsKind(err, failure.PersistenceUnavailable) {
			t.Fatal("forbidden role failure lost SQLSTATE or was accepted")
		}
		if err.Error() != "activate PostgreSQL capability role" {
			t.Fatal("driver message reached the safe error")
		}
	}
	if err := Configure(ctx, connection, config.User, CapabilityIssuer); err != nil {
		t.Fatal("registered issuer capability failed")
	}
	if err := Ensure(ctx, connection, config.User, CapabilityIssuer); err != nil {
		t.Fatal("exact issuer identity readback failed")
	}
}

// TestAssumeQueryUsesOnlyRegisteredStatements checks the five exact capability
// statements and rejects empty, unknown and SQL-shaped role inputs. It tests
// selection only, not SET ROLE execution, LOGIN grants or identity readback.
// TestAssumeQueryUsesOnlyRegisteredStatements проверяет пять точных capability
// statements и отклоняет пустое, неизвестное и SQL-подобное имя роли. Проверяется
// только выбор, не исполнение SET ROLE, права LOGIN и readback идентичности.
func TestAssumeQueryUsesOnlyRegisteredStatements(t *testing.T) {
	t.Parallel()
	queries, err := loadQueries()
	if err != nil {
		t.Fatalf("load session queries: %v", err)
	}
	for _, testCase := range []struct {
		capability string
		want       string
	}{
		{CapabilityIssuer, queries.issuerAssume},
		{CapabilityVerifier, queries.verifierAssume},
		{CapabilityPublisher, queries.publisherAssume},
		{CapabilityReadbackAttestor, queries.readbackAttestorAssume},
		{CapabilityRestoreController, queries.restoreControllerAssume},
		{"", ""},
		{"unregistered", ""},
		{"SET ROLE superuser;", ""},
	} {
		t.Run(testCase.capability, func(t *testing.T) {
			got, err := assumeQuery(queries, testCase.capability)
			if testCase.want == "" {
				if err == nil || got != "" {
					t.Fatal("unregistered capability selected SQL")
				}
				return
			}
			if err != nil || got != testCase.want {
				t.Fatalf("registered capability query changed: %v", err)
			}
		})
	}
}
