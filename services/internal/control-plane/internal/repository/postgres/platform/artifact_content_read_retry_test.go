package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type artifactReadCleanupStub struct {
	pgx.Tx
	rollback error
	calls    int
}

func (tx *artifactReadCleanupStub) Rollback(ctx context.Context) error {
	tx.calls++
	return tx.rollback
}

func TestArtifactContentReadRetryKeepsExhaustedFallbackAndCleanup(t *testing.T) {
	for _, fallback := range []error{errs.ErrUnavailable, errs.ErrConflict} {
		attempts, rollbacks := 0, 0
		_, err := retryArtifactContentRead(t.Context(), func() (_ platformrepo.ArtifactDownload, resultError error) {
			attempts++
			base := &artifactReadCleanupStub{}
			tx := &artifactContentReadTx{Tx: base}
			defer func() { tx.finish(t.Context(), &resultError); rollbacks += base.calls }()
			tx.observe(&pgconn.PgError{Code: "40001", Message: "PRIVATE_SQL_SENTINEL"})
			return platformrepo.ArtifactDownload{}, fallback
		})
		if attempts != 3 || rollbacks != 3 || !errors.Is(err, fallback) || strings.Contains(err.Error(), "PRIVATE_SQL_SENTINEL") {
			t.Fatal("retry bounds, cleanup or original fallback changed")
		}
	}
}

func TestArtifactContentReadRetryRejectsUnknownCommitDeadlockAndCleanupFailure(t *testing.T) {
	for _, test := range []struct {
		state           string
		cause, rollback error
		bodyFailed      bool
	}{
		{state: "40001", rollback: errors.New("PRIVATE_ROLLBACK_SENTINEL")},
		{state: "40001", bodyFailed: true},
		{state: "40P01"}, {state: "08006"}, {state: "23505"},
		{cause: pgx.ErrTxCommitRollback}, {cause: errors.New("PRIVATE_NETWORK_SENTINEL")},
	} {
		attempts := 0
		_, err := retryArtifactContentRead(t.Context(), func() (_ platformrepo.ArtifactDownload, resultError error) {
			attempts++
			tx := &artifactContentReadTx{Tx: &artifactReadCleanupStub{rollback: test.rollback}, bodyCleanupFailed: test.bodyFailed}
			defer tx.finish(t.Context(), &resultError)
			cause := test.cause
			if test.state != "" {
				cause = &pgconn.PgError{Code: test.state, Message: "PRIVATE_SQL_SENTINEL"}
			}
			tx.observe(cause)
			return platformrepo.ArtifactDownload{}, errs.ErrConflict
		})
		if attempts != 1 || !errors.Is(err, errs.ErrConflict) || strings.Contains(err.Error(), "PRIVATE") {
			t.Fatal("unknown outcome or failed cleanup retried")
		}
	}
}

func TestArtifactContentReadRetryRefreshesAuthorityAndStopsFreshDenial(t *testing.T) {
	for _, denial := range []error{errs.ErrForbidden, errs.ErrNotFound, errs.ErrInvalid, errs.ErrVersionMismatch} {
		attempts := 0
		result, err := retryArtifactContentRead(t.Context(), func() (platformrepo.ArtifactDownload, error) {
			attempts++
			if attempts == 1 {
				return platformrepo.ArtifactDownload{GrantRef: "discarded"}, &artifactReadSerializationRetry{fallback: errs.ErrUnavailable}
			}
			return platformrepo.ArtifactDownload{GrantRef: "not authorized"}, denial
		})
		if attempts != 2 || result.GrantRef != "" || !errors.Is(err, denial) {
			t.Fatal("fresh denial ignored or partial result escaped")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	attempts := 0
	_, err := retryArtifactContentRead(ctx, func() (platformrepo.ArtifactDownload, error) { attempts++; return platformrepo.ArtifactDownload{}, nil })
	if attempts != 0 || !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("canceled caller started another attempt")
	}
}

func TestArtifactContentReadRetryDoesNotCancelReturnedReaderContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	attempts := 0
	result, err := retryArtifactContentRead(ctx, func() (platformrepo.ArtifactDownload, error) {
		attempts++
		if attempts == 1 {
			return platformrepo.ArtifactDownload{}, &artifactReadSerializationRetry{fallback: errs.ErrConflict}
		}
		return platformrepo.ArtifactDownload{GrantRef: "committed"}, nil
	})
	if err != nil || attempts != 2 || result.GrantRef != "committed" || ctx.Err() != nil {
		t.Fatal("successful reader lifetime changed")
	}
}
