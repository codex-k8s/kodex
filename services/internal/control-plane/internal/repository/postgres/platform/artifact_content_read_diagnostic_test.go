package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/observability/artifactread"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestArtifactDownloadDiagnosticClassifiesDriverBeforeMapping checks nine synthetic driver,
// cancellation, storage, reference and joined-error cases before domain mapping, retaining only
// closed stage/class/SQLSTATE metadata. PRIVATE markers must never appear in the diagnostic; this
// is a local projection test without PostgreSQL or storage I/O.
// TestArtifactDownloadDiagnosticClassifiesDriverBeforeMapping проверяет девять синтетических
// driver, cancellation, storage, reference и joined-error исходов до доменного отображения,
// сохраняя только закрытые stage, class и SQLSTATE. PRIVATE маркеры не должны попадать в
// диагностику; это локальная проверка без PostgreSQL и storage I/O.
func TestArtifactDownloadDiagnosticClassifiesDriverBeforeMapping(t *testing.T) {
	for _, test := range []struct {
		stage        string
		cause        error
		class, state string
	}{
		{artifactread.StageAudit, fmt.Errorf("PRIVATE_TOKEN: %w", &pgconn.PgError{Code: "40001", Message: "PRIVATE_SQL", Detail: "PRIVATE_DSN"}), artifactread.ClassPostgres, "40001"},
		{artifactread.StageScope, &pgconn.PgError{Code: "PRIVATE_SQLSTATE", Message: "PRIVATE_SQL"}, artifactread.ClassPostgres, "OTHER"},
		{artifactread.StageBegin, context.DeadlineExceeded, artifactread.ClassDeadline, "NONE"},
		{artifactread.StageBegin, context.Canceled, artifactread.ClassCanceled, "NONE"},
		{artifactread.StageCommit, pgx.ErrTxCommitRollback, artifactread.ClassTransactionAbort, "NONE"},
		{artifactread.StageStorage, errors.New("PRIVATE_BUCKET_TOKEN"), artifactread.ClassStorage, "NONE"},
		{artifactread.StageSelect, errors.New("PRIVATE_QUERY_ARGUMENTS"), artifactread.ClassDriver, "NONE"},
		{artifactread.StageAuditRef, errors.New("PRIVATE_ENTROPY_FAILURE"), artifactread.ClassReference, "NONE"},
		{artifactread.StageAudit, errors.Join(&pgconn.PgError{Code: "40001"}, context.DeadlineExceeded), artifactread.ClassPostgres, "40001"},
	} {
		diagnostic := artifactReadDiagnostic(test.stage, test.cause)
		if diagnostic.Stage != test.stage || diagnostic.Class != test.class || diagnostic.SQLState != test.state || strings.Contains(fmt.Sprint(diagnostic), "PRIVATE") {
			t.Fatal("driver projection changed or disclosed raw diagnostic")
		}
	}
}

// artifactReadFaultRow implements a synthetic row carrying only a configured failure for
// transaction-observation tests. It never contains scanned values or opens a database connection.
// artifactReadFaultRow задаёт синтетическую строку только с указанной ошибкой для проверки
// наблюдения транзакции. Не содержит значений результата и не открывает соединение с БД.
type artifactReadFaultRow struct{ cause error }

// Scan returns the injected row failure without writing any destination, exposing the same raw
// cause to the diagnostic wrapper before safe mapping.
// Scan возвращает заданную ошибку строки без изменения destinations, передавая исходную причину
// диагностической обёртке до безопасного отображения.
func (row artifactReadFaultRow) Scan(...any) error { return row.cause }

// artifactReadFaultRows embeds unused pgx.Rows while overriding only Scan and Err with the same
// injected failure. Unexpected unimplemented operations cannot silently succeed; no real result
// set is allocated.
// artifactReadFaultRows встраивает неиспользуемый pgx.Rows, переопределяя только Scan и Err
// заданной ошибкой. Непредусмотренные операции не могут незаметно завершиться успешно; настоящего
// набора строк нет.
type artifactReadFaultRows struct {
	pgx.Rows
	cause error
}

// Scan returns the configured synthetic scan error without assigning fields, allowing the
// row-boundary observer to retain its closed diagnostic.
// Scan возвращает синтетическую ошибку Scan без присваивания полей, позволяя наблюдателю сохранить
// закрытую диагностику границы строки.
func (rows artifactReadFaultRows) Scan(...any) error { return rows.cause }

// Err returns the same injected iteration failure for diagnostic classification; it does not
// advance rows or change cleanup state.
// Err возвращает заданную ошибку iteration для классификации, не перемещая rows и не меняя cleanup
// state.
func (rows artifactReadFaultRows) Err() error { return rows.cause }

// artifactReadFaultTx combines a rollback-counting cleanup stub with an injected failure for all
// overridden SQL boundaries. It observes local adapter behavior, not a real transaction or a
// committed effect.
// artifactReadFaultTx связывает cleanup stub со счётчиком rollback и заданную ошибку всех
// переопределённых SQL-границ. Наблюдает локальный adapter, не настоящую транзакцию и commit.
type artifactReadFaultTx struct {
	artifactReadCleanupStub
	cause error
}

// QueryRow returns a failing synthetic row so the diagnostic can observe an error at Scan time,
// without executing SQL or interpreting the supplied arguments.
// QueryRow возвращает ошибочную синтетическую строку, чтобы диагностика увидела ошибку при Scan,
// без исполнения SQL и разбора аргументов.
func (tx *artifactReadFaultTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return artifactReadFaultRow{tx.cause}
}

// Query returns no rows plus the configured query failure; it performs no database operation and
// leaves rollback tracking to the embedded cleanup stub.
// Query возвращает nil rows и заданную ошибку запроса, не обращаясь к БД; rollback учитывает
// вложенный cleanup stub.
func (tx *artifactReadFaultTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, tx.cause
}

// Exec returns a zero command tag and the injected execution error, allowing classification before
// domain mapping without a stored write.
// Exec возвращает нулевой command tag и заданную ошибку исполнения для классификации до доменного
// отображения, без записи состояния.
func (tx *artifactReadFaultTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, tx.cause
}

// Commit returns the configured synthetic commit failure without committing; cleanup and bounded
// retry decisions are exercised by the surrounding fixture.
// Commit возвращает синтетическую ошибку commit без фиксации; cleanup и ограниченный повтор
// проверяет внешняя оснастка.
func (tx *artifactReadFaultTx) Commit(context.Context) error { return tx.cause }

// TestArtifactDownloadDiagnosticSurvivesEverySQLBoundaryAndBoundedArtifactRetries injects
// serialization failure through six query/scan/iteration/exec/commit boundaries and requires exact
// retry/rollback counts, exhausted attempt metadata and the original domain outcome. Raw
// PostgreSQL causes and PRIVATE payloads must not escape; immediate synthetic backoff proves
// bounds, not live concurrency.
// TestArtifactDownloadDiagnosticSurvivesEverySQLBoundaryAndBoundedArtifactRetries вводит
// serialization failure в шесть query, scan, iteration, exec и commit границ и требует точных
// retry и rollback counts, exhausted attempt metadata и прежнего доменного исхода. Сырые
// PostgreSQL causes и PRIVATE данные не выдаются; мгновенный backoff доказывает пределы, не живую
// конкуренцию.
func TestArtifactDownloadDiagnosticSurvivesEverySQLBoundaryAndBoundedArtifactRetries(t *testing.T) {
	for _, operation := range []string{"query_row", "query", "row_scan", "rows_error", "exec", "commit"} {
		t.Run(operation, func(t *testing.T) {
			attempts, rollbacks := 0, 0
			outcome := errs.ErrUnavailable
			stage := artifactread.StageAudit
			if operation == "commit" {
				outcome, stage = errs.ErrConflict, artifactread.StageCommit
			}
			_, err := retryArtifactContentReadWithBackoff(t.Context(), func() (_ platformrepo.ArtifactDownload, resultError error) {
				attempts++
				base := &artifactReadFaultTx{cause: &pgconn.PgError{Code: "40001", Message: "PRIVATE_SQL", InternalQuery: "PRIVATE_QUERY"}}
				tx := &artifactContentReadTx{Tx: base, stage: stage}
				defer func() { tx.finish(t.Context(), &resultError); rollbacks += base.calls }()
				switch operation {
				case "query_row":
					_ = tx.QueryRow(t.Context(), "fixture").Scan()
				case "query":
					_, _ = tx.Query(t.Context(), "fixture")
				case "row_scan":
					_ = artifactContentReadRows{Rows: artifactReadFaultRows{cause: base.cause}, tx: tx}.Scan()
				case "rows_error":
					_ = artifactContentReadRows{Rows: artifactReadFaultRows{cause: base.cause}, tx: tx}.Err()
				case "exec":
					_, _ = tx.Exec(t.Context(), "fixture")
				case "commit":
					_ = tx.Commit(t.Context())
				}
				return platformrepo.ArtifactDownload{}, outcome
			}, artifactReadImmediateBackoff)
			diagnostic, ok := artifactread.From(err)
			if !ok || attempts != artifactContentReadAttempts || rollbacks != artifactContentReadAttempts || !errors.Is(err, outcome) || diagnostic.Stage != stage || diagnostic.Class != artifactread.ClassPostgres || diagnostic.SQLState != "40001" || diagnostic.Attempt != artifactContentReadAttempts || !diagnostic.RetryExhausted {
				t.Fatal("SQL observation, retry bounds or exhausted outcome changed")
			}
			var raw *pgconn.PgError
			if errors.As(err, &raw) || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("raw PostgreSQL cause escaped")
			}
		})
	}
}

// TestArtifactDownloadDiagnosticDoesNotExpandRetryEligibility requires one attempt for
// rollback/body-cleanup failure, storage failure, deadlock, unknown state and connection error
// even when diagnostic metadata is present. It preserves Unavailable and closed diagnostics
// without treating an observed cause as permission to retry.
// TestArtifactDownloadDiagnosticDoesNotExpandRetryEligibility требует одну попытку при rollback и
// body-cleanup failure, storage failure, deadlock, неизвестном состоянии и connection error даже
// при наличии metadata диагностики. Сохраняет Unavailable и закрытую диагностику без разрешения
// повтора по одному наблюдению.
func TestArtifactDownloadDiagnosticDoesNotExpandRetryEligibility(t *testing.T) {
	for _, test := range []struct {
		state               string
		rollback            error
		bodyFailed, storage bool
	}{
		{state: "40001", rollback: errors.New("PRIVATE_ROLLBACK")},
		{state: "40001", bodyFailed: true},
		{state: "40001", storage: true},
		{state: "40P01"}, {state: "PRIVATE_STATE"}, {state: "08006"},
	} {
		attempts := 0
		_, err := retryArtifactContentRead(t.Context(), func() (_ platformrepo.ArtifactDownload, resultError error) {
			attempts++
			tx := &artifactContentReadTx{Tx: &artifactReadCleanupStub{rollback: test.rollback}, stage: artifactread.StageAudit, bodyCleanupFailed: test.bodyFailed}
			defer tx.finish(t.Context(), &resultError)
			cause := &pgconn.PgError{Code: test.state, Message: "PRIVATE_SQL"}
			if test.storage {
				tx.stage = artifactread.StageStorage
				tx.observeFailure(cause)
			} else {
				tx.observe(cause)
			}
			return platformrepo.ArtifactDownload{}, errs.ErrUnavailable
		})
		diagnostic, ok := artifactread.From(err)
		if !ok || attempts != 1 || diagnostic.Attempt != 1 || diagnostic.RetryExhausted || !errors.Is(err, errs.ErrUnavailable) || strings.Contains(fmt.Sprint(diagnostic), "PRIVATE") {
			t.Fatal("diagnostic expanded retry eligibility or disclosed private state")
		}
	}
}
