package platform

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type assistantReadRollbackStub struct {
	pgx.Tx
	events *[]string
	err    error
}

func (tx assistantReadRollbackStub) Rollback(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	*tx.events = append(*tx.events, "rollback")
	return tx.err
}

func TestAssistantLockedReadRetriesFreshWholeAttemptAfterRollback(t *testing.T) {
	events := []string{}
	attempts := 0
	result, err := retryAssistantLockedRead(t.Context(), func(ctx context.Context) (_ string, resultError error) {
		attempts++
		events = append(events, "begin", "scope", "lease", "fence", "owner")
		defer rollbackAssistantLockedRead(ctx, assistantReadRollbackStub{events: &events}, &resultError)
		if attempts == 1 {
			return "partial forbidden snapshot", assistantLockedReadError(&pgconn.PgError{Code: "40001", Message: "PRIVATE_SQL_SENTINEL"}, errs.ErrUnavailable)
		}
		events = append(events, "snapshot", "commit")
		return "fresh committed snapshot", nil
	})
	want := []string{"begin", "scope", "lease", "fence", "owner", "rollback", "begin", "scope", "lease", "fence", "owner", "snapshot", "commit", "rollback"}
	if err != nil || result != "fresh committed snapshot" || attempts != 2 || !slices.Equal(events, want) {
		t.Fatal("whole authority read did not restart after rollback")
	}
}

func TestAssistantLockedReadRetriesOnlyExplicit40001(t *testing.T) {
	for _, cause := range []error{
		&pgconn.PgError{Code: "40001"}, &pgconn.PgError{Code: "40P01"}, &pgconn.PgError{Code: "23505"},
		&pgconn.PgError{Code: "08006"}, &pgconn.PgError{Code: "57014"}, pgx.ErrTxCommitRollback,
		errs.ErrForbidden, errs.ErrNotFound, errs.ErrInvalid, errors.New("PRIVATE_SQL_SENTINEL"),
	} {
		t.Run(fmt.Sprintf("%T_%s", cause, func() string {
			var p *pgconn.PgError
			if errors.As(cause, &p) {
				return p.Code
			}
			return "domain"
		}()), func(t *testing.T) {
			attempts := 0
			_, err := retryAssistantLockedRead(t.Context(), func(context.Context) (string, error) {
				attempts++
				return "partial", assistantLockedReadError(fmt.Errorf("wrapped: %w", cause), errs.ErrUnavailable)
			})
			want := 1
			var pgError *pgconn.PgError
			if errors.As(cause, &pgError) && pgError.Code == "40001" {
				want = 3
			}
			if attempts != want || !errors.Is(err, errs.ErrUnavailable) || strings.Contains(err.Error(), "PRIVATE_SQL_SENTINEL") {
				t.Fatal("read retry classification, bound or redaction changed")
			}
		})
	}
}

func TestAssistantLockedReadRechecksRevokeAndDropsPartialResult(t *testing.T) {
	for _, denied := range []error{errs.ErrNotFound, errs.ErrForbidden, errs.ErrInvalid} {
		attempts := 0
		result, err := retryAssistantLockedRead(t.Context(), func(context.Context) (string, error) {
			attempts++
			if attempts == 1 {
				return "old", assistantLockedReadError(&pgconn.PgError{Code: "40001"}, errs.ErrUnavailable)
			}
			return "not authorized", denied
		})
		if attempts != 2 || result != "" || !errors.Is(err, denied) {
			t.Fatal("fresh denial was retried or partial authority escaped")
		}
	}
}

func TestAssistantLockedReadPreservesCallerCancellationAndDeadline(t *testing.T) {
	for _, cancelBefore := range []bool{true, false} {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		deadline, _ := ctx.Deadline()
		if cancelBefore {
			cancel()
		}
		attempts := 0
		_, err := retryAssistantLockedRead(ctx, func(attemptCtx context.Context) (string, error) {
			attempts++
			actual, _ := attemptCtx.Deadline()
			if !actual.Equal(deadline) {
				t.Fatal("caller deadline replaced")
			}
			cancel()
			return "partial", assistantLockedReadError(&pgconn.PgError{Code: "40001"}, errs.ErrUnavailable)
		})
		cancel()
		want := 1
		if cancelBefore {
			want = 0
		}
		if attempts != want || !errors.Is(err, errs.ErrUnavailable) {
			t.Fatal("canceled caller started another attempt")
		}
	}
}

func TestAssistantLockedReadRollbackFailurePreventsRetry(t *testing.T) {
	events := []string{}
	attempts := 0
	result, err := retryAssistantLockedRead(t.Context(), func(ctx context.Context) (_ string, resultError error) {
		attempts++
		defer rollbackAssistantLockedRead(ctx, assistantReadRollbackStub{events: &events, err: errors.New("PRIVATE_ROLLBACK_SENTINEL")}, &resultError)
		return "partial", assistantLockedReadError(&pgconn.PgError{Code: "40001"}, errs.ErrUnavailable)
	})
	if attempts != 1 || result != "" || !errors.Is(err, errs.ErrUnavailable) || !slices.Equal(events, []string{"rollback"}) {
		t.Fatal("retry continued after failed rollback")
	}
}
