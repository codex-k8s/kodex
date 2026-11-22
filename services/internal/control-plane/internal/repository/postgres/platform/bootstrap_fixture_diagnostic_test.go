package platform

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// bootstrapFixtureDiagnostic exposes only source-owned phases, validated SQLSTATE and a closed semantic label.
// It never includes driver Message, Detail, SQL, arguments, table names or credential material.
// RU: bootstrapFixtureDiagnostic раскрывает лишь source-owned phases, проверенный SQLSTATE и closed semantic label.
// Driver Message, Detail, SQL, arguments, table names и credential material не включаются.
func bootstrapFixtureDiagnostic(failure error) (string, string, string) {
	phase, code, reason := "unclassified", "none", "unclassified"
	pending := []error{failure}
	for visited := 0; len(pending) > 0 && visited < 32; visited++ {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if current == nil {
			continue
		}
		switch typed := current.(type) {
		case *bootstrapError:
			if typed.phase != bootstrapTransactionPhase {
				phase = typed.phase
			}
		case *pgconn.PgError:
			valid := len(typed.Code) == 5
			for _, r := range typed.Code {
				valid = valid && (r >= '0' && r <= '9' || r >= 'A' && r <= 'Z')
			}
			if valid {
				code = typed.Code
			}
		default:
			if current.Error() == "integration definition revision rollback or digest mismatch" {
				reason = "DEFINITION_PIN_MISMATCH"
			}
		}
		if joined, ok := current.(interface{ Unwrap() []error }); ok {
			pending = append(pending, joined.Unwrap()...)
		} else if wrapped := errors.Unwrap(current); wrapped != nil {
			pending = append(pending, wrapped)
		}
	}
	return phase, code, reason
}

// TestBootstrapFixtureDiagnosticKeepsDriverPayloadPrivate checks safe classification of nested bootstrap failures.
// RU: TestBootstrapFixtureDiagnosticKeepsDriverPayloadPrivate проверяет безопасную классификацию вложенных bootstrap failures.
func TestBootstrapFixtureDiagnosticKeepsDriverPayloadPrivate(t *testing.T) {
	driver := &pgconn.PgError{Code: "42P01", Message: "PRIVATE_DRIVER_SENTINEL", Detail: "PRIVATE_DETAIL_SENTINEL"}
	failure := bootstrapFailure(bootstrapTransactionPhase, &bootstrapAttemptFailure{cause: bootstrapFailure("reconcile integration definition", driver)})
	phase, code, reason := bootstrapFixtureDiagnostic(failure)
	if phase != "reconcile integration definition" || code != "42P01" || reason != "unclassified" {
		t.Fatal("bootstrap diagnostic lost safe classification")
	}
	phase, code, reason = bootstrapFixtureDiagnostic(errors.New("integration definition revision rollback or digest mismatch"))
	if phase != "unclassified" || code != "none" || reason != "DEFINITION_PIN_MISMATCH" {
		t.Fatal("bootstrap semantic diagnostic changed")
	}
}
