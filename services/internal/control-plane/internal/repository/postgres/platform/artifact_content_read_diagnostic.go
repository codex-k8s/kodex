package platform

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/observability/artifactread"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// artifactReadDiagnostic returns normalized closed diagnostic metadata without retaining the
// driver error. Storage stage wins immediately; otherwise grant/audit-ref stages default to
// Reference and other stages to Driver, then wrapped PostgreSQL, canceled, deadline and
// commit-rollback causes override in that order. NewDiagnostic normalizes stage/class/SQLSTATE;
// the helper neither logs raw details nor proves retry eligibility, rollback or an owner commit.
// artifactReadDiagnostic возвращает нормализованную закрытую diagnostic metadata без сохранения
// driver error. Storage stage имеет немедленный приоритет; иначе grant/audit-ref stages получают
// Reference, прочие — Driver, затем wrapped PostgreSQL, canceled, deadline и commit-rollback
// causes переопределяют класс в таком порядке. NewDiagnostic нормализует stage/class/SQLSTATE;
// helper не логирует сырые данные и не доказывает retry eligibility, rollback или owner commit.
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
