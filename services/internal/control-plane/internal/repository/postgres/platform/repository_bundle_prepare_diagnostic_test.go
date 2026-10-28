package platform

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestRepositoryBundlePrepareDiagnostic preserves closed error/retry identity and prevents database/provider messages or payloads from entering printable or unwrapped diagnostic chains.
// TestRepositoryBundlePrepareDiagnostic сохраняет closed error/retry identity и не допускает database/provider messages либо payloads в printable или unwrapped diagnostic chains.
func TestRepositoryBundlePrepareDiagnostic(t *testing.T) {
	for _, v := range []struct{ code, category string }{
		{"0A000", "UNSUPPORTED_SQL"}, {"42501", "SQL_PERMISSION_DENIED"}, {"42703", "UNDEFINED_COLUMN"}, {"42P01", "UNDEFINED_RELATION"}, {"42883", "UNDEFINED_FUNCTION"}, {"40001", "SERIALIZATION_RETRY"}, {"23514", "CONSTRAINT_REJECTED"}, {"unexpected-private-value", "OTHER_SQL"},
	} {
		raw := &pgconn.PgError{Code: v.code, Message: "synthetic private payload must not escape", Detail: strings.Repeat("synthetic private detail", 200), ConstraintName: "synthetic-private-name"}
		mapped := serializableTransactionError(raw, errs.ErrUnavailable)
		err := repositoryBundlePrepareFailure("ARTIFACT_ELIGIBILITY", raw, mapped)
		if !errors.Is(err, errs.ErrUnavailable) || err.Error() != "repository bundle prepare failed: stage=ARTIFACT_ELIGIBILITY class="+v.category || len(err.Error()) > 160 {
			t.Fatal("unsafe or incorrect prepare diagnostic")
		}
		var original *pgconn.PgError
		if errors.As(err, &original) {
			t.Fatal("raw database cause retained")
		}
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			formatted := fmt.Sprintf(format, err)
			if strings.Contains(formatted, "synthetic") || strings.Contains(formatted, "PgError") || len(formatted) > 180 {
				t.Fatal("debug formatter leaked error payload")
			}
		}
		if errors.Is(err, errSerializableTransactionRetry) != (v.code == "40001") {
			t.Fatal("serializable retry identity changed")
		}
	}
	err := repositoryBundlePrepareFailure("untrusted-stage-body", errs.ErrForbidden, errs.ErrForbidden)
	if err.Error() != "repository bundle prepare failed: stage=UNKNOWN class=AUTHORITY_DENIED" || !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("unknown stage reflected or authority changed")
	}
	err = repositoryBundlePrepareFailure("NATIVE_INTENT", errors.New("synthetic provider payload"), errs.ErrIdempotencyReuse)
	if !errors.Is(err, errs.ErrIdempotencyReuse) || strings.Contains(err.Error(), "synthetic") {
		t.Fatal("native error identity changed or private text leaked")
	}
	if repositoryBundlePrepareFailure("ORIGIN", nil, errs.ErrUnavailable) != nil {
		t.Fatal("nil failure created an error")
	}
}
