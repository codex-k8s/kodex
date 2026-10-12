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

type artifactReadFaultRow struct{ cause error }

func (row artifactReadFaultRow) Scan(...any) error { return row.cause }

type artifactReadFaultRows struct {
	pgx.Rows
	cause error
}

func (rows artifactReadFaultRows) Scan(...any) error { return rows.cause }
func (rows artifactReadFaultRows) Err() error        { return rows.cause }

type artifactReadFaultTx struct {
	artifactReadCleanupStub
	cause error
}

func (tx *artifactReadFaultTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return artifactReadFaultRow{tx.cause}
}
func (tx *artifactReadFaultTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, tx.cause
}
func (tx *artifactReadFaultTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, tx.cause
}
func (tx *artifactReadFaultTx) Commit(context.Context) error { return tx.cause }

func TestArtifactDownloadDiagnosticSurvivesEverySQLBoundaryAndThreeRetries(t *testing.T) {
	for _, operation := range []string{"query_row", "query", "row_scan", "rows_error", "exec", "commit"} {
		t.Run(operation, func(t *testing.T) {
			attempts, rollbacks := 0, 0
			outcome := errs.ErrUnavailable
			stage := artifactread.StageAudit
			if operation == "commit" {
				outcome, stage = errs.ErrConflict, artifactread.StageCommit
			}
			_, err := retryArtifactContentRead(t.Context(), func() (_ platformrepo.ArtifactDownload, resultError error) {
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
			})
			diagnostic, ok := artifactread.From(err)
			if !ok || attempts != 3 || rollbacks != 3 || !errors.Is(err, outcome) || diagnostic.Stage != stage || diagnostic.Class != artifactread.ClassPostgres || diagnostic.SQLState != "40001" || diagnostic.Attempt != 3 || !diagnostic.RetryExhausted {
				t.Fatal("SQL observation, retry bounds or exhausted outcome changed")
			}
			var raw *pgconn.PgError
			if errors.As(err, &raw) || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("raw PostgreSQL cause escaped")
			}
		})
	}
}

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
