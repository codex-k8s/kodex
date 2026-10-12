// Package artifactread переносит только закрытую диагностику owner DOWNLOAD.
package artifactread

import (
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

const (
	StageUnknown      = "UNCLASSIFIED"
	StageScope        = "SCOPE"
	StageBegin        = "BEGIN"
	StageSelect       = "SELECT_REVISION"
	StageMetadata     = "ARTIFACT_METADATA"
	StageEligibility  = "ELIGIBILITY"
	StageRevision     = "REVISION_METADATA"
	StageGrantRef     = "GRANT_REF"
	StageGrantCreate  = "GRANT_CREATE"
	StageGrantConsume = "GRANT_CONSUME"
	StageContent      = "CONTENT_RECEIPT"
	StageStorage      = "STORAGE_GET"
	StageAuditRef     = "AUDIT_REF"
	StageAudit        = "AUDIT_INSERT"
	StageCommit       = "COMMIT"
	StageRetry        = "RETRY_BUDGET"

	ClassUnknown          = "UNCLASSIFIED"
	ClassPostgres         = "POSTGRES"
	ClassDriver           = "DRIVER"
	ClassCanceled         = "CANCELED"
	ClassDeadline         = "DEADLINE"
	ClassTransactionAbort = "TRANSACTION_ABORT"
	ClassDomain           = "DOMAIN"
	ClassStorage          = "STORAGE"
	ClassReference        = "REFERENCE"
)

// Diagnostic не содержит исходную ошибку, SQL, параметры или координаты ресурса.
type Diagnostic struct {
	Stage, Class, SQLState string
	Attempt                int
	RetryExhausted         bool
}

type failure struct {
	cause      error
	diagnostic Diagnostic
}

// NewDiagnostic закрывает значения до сохранения проекции в adapter.
func NewDiagnostic(stage, class, sqlState string) Diagnostic {
	return normalize(Diagnostic{Stage: stage, Class: class, SQLState: sqlState})
}

func (err *failure) Error() string { return err.cause.Error() }
func (err *failure) Unwrap() error { return err.cause }

// WithFailure принимает уже отображённый domain outcome. В цепочке остаётся
// только canonical sentinel, даже если переданный outcome обёрнут сырой ошибкой.
func WithFailure(outcome error, diagnostic Diagnostic) error {
	var cause error
	switch {
	case errors.Is(outcome, errs.ErrUnavailable):
		cause = errs.ErrUnavailable
	case errors.Is(outcome, errs.ErrConflict):
		cause = errs.ErrConflict
	default:
		return outcome
	}
	return &failure{cause: cause, diagnostic: normalize(diagnostic)}
}

func WithAttempt(err error, attempt int, exhausted bool) error {
	diagnostic, ok := From(err)
	if !ok {
		return err
	}
	diagnostic.Attempt, diagnostic.RetryExhausted = attempt, exhausted
	return WithFailure(err, diagnostic)
}

// From не доверяет произвольному интерфейсу ошибки и повторно закрывает поля.
func From(err error) (Diagnostic, bool) {
	var source *failure
	if !errors.As(err, &source) {
		return Diagnostic{}, false
	}
	return normalize(source.diagnostic), true
}

func normalize(value Diagnostic) Diagnostic {
	switch value.Stage {
	case StageScope, StageBegin, StageSelect, StageMetadata, StageEligibility,
		StageRevision, StageGrantRef, StageGrantCreate, StageGrantConsume, StageContent,
		StageStorage, StageAuditRef, StageAudit, StageCommit, StageRetry:
	default:
		value.Stage = StageUnknown
	}
	switch value.Class {
	case ClassPostgres, ClassDriver, ClassCanceled, ClassDeadline,
		ClassTransactionAbort, ClassDomain, ClassStorage, ClassReference:
	default:
		value.Class = ClassUnknown
	}
	if value.Class != ClassPostgres {
		value.SQLState = "NONE"
	} else {
		switch value.SQLState {
		case "40001", "40P01", "08006", "23505", "42501", "42P01", "42883", "57014", "53300":
		default:
			value.SQLState = "OTHER"
		}
	}
	if value.Attempt < 0 || value.Attempt > 3 {
		value.Attempt = 0
	}
	return value
}
