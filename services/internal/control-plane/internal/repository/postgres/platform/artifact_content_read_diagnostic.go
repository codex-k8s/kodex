package platform

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/observability/artifactread"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Driver cause используется только для закрытой проекции и не сохраняется.
func artifactReadDiagnostic(stage string, cause error) artifactread.Diagnostic {
	if stage == artifactread.StageStorage {
		return artifactread.NewDiagnostic(stage, artifactread.ClassStorage, "")
	}
	diagnostic := artifactread.Diagnostic{Stage: stage, Class: artifactread.ClassDriver}
	switch stage {
	case artifactread.StageGrantRef, artifactread.StageAuditRef:
		diagnostic.Class = artifactread.ClassReference
	}
	var postgres *pgconn.PgError
	switch {
	case errors.As(cause, &postgres):
		diagnostic.Class, diagnostic.SQLState = artifactread.ClassPostgres, postgres.Code
	case errors.Is(cause, context.Canceled):
		diagnostic.Class = artifactread.ClassCanceled
	case errors.Is(cause, context.DeadlineExceeded):
		diagnostic.Class = artifactread.ClassDeadline
	case errors.Is(cause, pgx.ErrTxCommitRollback):
		diagnostic.Class = artifactread.ClassTransactionAbort
	}
	return artifactread.NewDiagnostic(diagnostic.Stage, diagnostic.Class, diagnostic.SQLState)
}
