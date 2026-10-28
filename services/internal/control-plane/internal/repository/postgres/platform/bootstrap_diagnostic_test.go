package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// bootstrapFaultRow returns one private synthetic driver failure from the real caller's Scan.
// bootstrapFaultRow возвращает приватный синтетический отказ драйвера из настоящего Scan вызывающего.
type bootstrapFaultRow struct{ cause error }

// Scan preserves the selected private failure without inspecting SQL destinations or values.
// Scan сохраняет выбранный приватный отказ без чтения SQL destinations и значений.
func (row bootstrapFaultRow) Scan(...any) error { return row.cause }

// bootstrapFaultTx injects only the selected read or write boundary; no database is accessed.
// bootstrapFaultTx подставляет отказ лишь выбранной read/write границы, не обращаясь к базе.
type bootstrapFaultTx struct {
	pgx.Tx
	cause    error
	failRead bool
}

// QueryRow fails the first read or reports absence so the real integration upsert path is reached.
// QueryRow отклоняет первое чтение либо сообщает отсутствие для настоящего integration upsert path.
func (tx bootstrapFaultTx) QueryRow(context.Context, string, ...any) pgx.Row {
	if tx.failRead {
		return bootstrapFaultRow{tx.cause}
	}
	return bootstrapFaultRow{pgx.ErrNoRows}
}

// Exec fails the exact real write and never fabricates a successful command tag.
// Exec отклоняет настоящий write, не создавая успешного command tag.
func (tx bootstrapFaultTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, tx.cause
}

// TestBootstrapReconciliationPreservesPrivateCauses exercises actual reconciliation boundaries,
// keeping SQLSTATE/context inspection private while preventing raw driver text in Error output.
// TestBootstrapReconciliationPreservesPrivateCauses проверяет настоящие reconciliation границы,
// сохраняя приватный SQLSTATE/context без сырого текста драйвера в Error.
func TestBootstrapReconciliationPreservesPrivateCauses(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal("load shipped fixture definitions")
	}
	for _, cause := range []error{
		&pgconn.PgError{Code: "40001", Message: "PRIVATE_SQL_PASSWORD_SENTINEL", ConstraintName: "PRIVATE_CONSTRAINT_SENTINEL"},
		&pgconn.PgError{Code: "40P01", Message: "PRIVATE_SQL_PASSWORD_SENTINEL"},
		&pgconn.PgError{Code: "42501", Message: "PRIVATE_SQL_PASSWORD_SENTINEL"},
		context.DeadlineExceeded,
	} {
		for _, phase := range []string{"provider", "core", "environment", "integration-read", "integration-write", "integration-disable"} {
			t.Run(phase, func(t *testing.T) {
				r := &Repository{integrationDefinitions: definitions}
				tx := bootstrapFaultTx{cause: cause, failRead: true}
				var failure error
				switch phase {
				case "provider":
					failure = r.reconcileProviderCredential(t.Context(), tx)
				case "core":
					failure = r.reconcileSystemAssistantCorePrompt(t.Context(), tx, "revision-1", "fixture")
				case "environment":
					failure = r.reconcileSystemAssistantRuntimeEnvironment(t.Context(), tx)
				case "integration-read":
					failure = r.reconcileIntegrationDefinitions(t.Context(), tx)
				case "integration-write":
					tx.failRead = false
					failure = r.reconcileIntegrationDefinitions(t.Context(), tx)
				case "integration-disable":
					r.integrationDefinitions = nil
					failure = r.reconcileIntegrationDefinitions(t.Context(), tx)
				}
				if !errors.Is(failure, cause) {
					t.Fatal("bootstrap reconciliation lost private typed cause")
				}
				if strings.Contains(failure.Error(), "PRIVATE_") {
					t.Fatal("bootstrap reconciliation disclosed driver payload")
				}
			})
		}
	}
}
