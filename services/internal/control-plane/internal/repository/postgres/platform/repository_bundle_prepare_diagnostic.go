package platform

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// repositoryBundlePrepareDiagnosticFormat contains only closed stage/category identifiers; SQL text, database messages, constraints, locators and artifact bytes are never formatted.
// repositoryBundlePrepareDiagnosticFormat содержит лишь closed stage/category identifiers; SQL text, database messages, constraints, locators и artifact bytes не форматируются.
const repositoryBundlePrepareDiagnosticFormat = "repository bundle prepare failed: stage=%s class=%s"

// repositoryBundlePrepareDiagnostic preserves the mapped domain/retry identity while its printable form excludes the original database/provider error and all caller input.
// repositoryBundlePrepareDiagnostic сохраняет mapped domain/retry identity, исключая из printable form исходную database/provider error и весь caller input.
type repositoryBundlePrepareDiagnostic struct {
	stage, category string
	cause           error
}

// Error exposes only fixed bounded diagnostic identifiers, including when an enclosing service logs the error with its ordinary formatter.
// Error выдаёт лишь fixed bounded diagnostic identifiers, в том числе при обычном форматировании ошибки enclosing service.
func (e repositoryBundlePrepareDiagnostic) Error() string {
	return fmt.Sprintf(repositoryBundlePrepareDiagnosticFormat, e.stage, e.category)
}

// Format prevents debug formatting from reflecting the wrapper or its cause; every verb emits only the same closed stage/class text, optionally quoted.
// Format исключает reflection wrapper либо cause при debug formatting; любой verb выдаёт лишь тот же closed stage/class text, при необходимости в кавычках.
func (e repositoryBundlePrepareDiagnostic) Format(state fmt.State, verb rune) {
	if verb == 'q' {
		_, _ = fmt.Fprintf(state, "%q", e.Error())
		return
	}
	_, _ = io.WriteString(state, e.Error())
}

// Unwrap retains errors.Is behavior for authority denial and serializable retry; the safe printable representation never formats this cause.
// Unwrap сохраняет errors.Is для authority denial и serializable retry; safe printable representation эту cause не форматирует.
func (e repositoryBundlePrepareDiagnostic) Unwrap() error { return e.cause }

// repositoryBundlePrepareFailure classifies one preparation boundary without changing transaction behavior or retaining the raw SQL/provider error. Unknown stages and SQLSTATEs collapse to a fixed fallback.
// repositoryBundlePrepareFailure классифицирует одну preparation boundary, не меняя transaction behavior и не сохраняя raw SQL/provider error. Unknown stages и SQLSTATEs сворачиваются в fixed fallback.
func repositoryBundlePrepareFailure(stage string, original, mapped error) error {
	if original == nil {
		return nil
	}
	switch stage {
	case "ARCHIVE_READ", "MANIFEST_READ", "DECODE", "DECODED_SCAN", "SCOPE", "BEGIN", "ORIGIN", "REPLAY", "ARTIFACT_ELIGIBILITY", "CONNECTION", "DEFINITION", "CREDENTIAL", "NATIVE_INTENT", "IDENTITY", "CLOCK", "RESOURCE_SCOPE", "SOURCE_PIN", "INSERT", "AUDIT", "COMMIT":
	default:
		stage = "UNKNOWN"
	}
	category := "UNAVAILABLE"
	var database *pgconn.PgError
	if errors.As(original, &database) {
		switch database.Code {
		case "42703":
			category = "UNDEFINED_COLUMN"
		case "42P01":
			category = "UNDEFINED_RELATION"
		case "42883":
			category = "UNDEFINED_FUNCTION"
		case "42601":
			category = "INVALID_SQL"
		case "0A000":
			category = "UNSUPPORTED_SQL"
		case "42501":
			category = "SQL_PERMISSION_DENIED"
		case "40001":
			category = "SERIALIZATION_RETRY"
		case "40P01":
			category = "DEADLOCK_RETRY"
		case "55P03":
			category = "LOCK_BUSY"
		case "57014":
			category = "QUERY_CANCELLED"
		case "23505", "23503", "23514", "23502":
			category = "CONSTRAINT_REJECTED"
		default:
			category = "OTHER_SQL"
		}
	} else {
		switch {
		case errors.Is(original, pgx.ErrNoRows):
			category = "NO_MATCH"
		case errors.Is(original, context.DeadlineExceeded), errors.Is(original, context.Canceled):
			category = "CONTEXT_CLOSED"
		case errors.Is(original, errs.ErrForbidden):
			category = "AUTHORITY_DENIED"
		case errors.Is(original, errs.ErrConflict), errors.Is(original, errs.ErrVersionMismatch):
			category = "CONFLICT"
		case errors.Is(original, errs.ErrInvalid):
			category = "INVALID_INPUT"
		}
	}
	cause := errs.ErrUnavailable
	for _, candidate := range []error{errs.ErrUnauthorized, errs.ErrFreshAuthenticationRequired, errs.ErrForbidden, errs.ErrNotFound, errs.ErrVersionMismatch, errs.ErrIdempotencyReuse, errs.ErrProtected, errs.ErrResourceInUse, errs.ErrCapabilityRequired, errs.ErrConflict, errs.ErrInvalid, errs.ErrUnavailable} {
		if errors.Is(mapped, candidate) {
			cause = candidate
			break
		}
	}
	if errors.Is(mapped, errSerializableTransactionRetry) {
		cause = errors.Join(errSerializableTransactionRetry, cause)
	}
	return repositoryBundlePrepareDiagnostic{stage: stage, category: category, cause: cause}
}
