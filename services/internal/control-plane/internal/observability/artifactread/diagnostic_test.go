package artifactread

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// TestArtifactDownloadDiagnosticClosesUnknownValuesAndDropsRawCause proves stage/class/SQLSTATE/attempt normalization and retention of only a canonical domain cause even when the original error contains synthetic private SQL/token-like text.
// TestArtifactDownloadDiagnosticClosesUnknownValuesAndDropsRawCause доказывает нормализацию stage/class/SQLSTATE/attempt и сохранение лишь канонической доменной причины даже при синтетическом приватном SQL/token-like тексте исходной ошибки.
func TestArtifactDownloadDiagnosticClosesUnknownValuesAndDropsRawCause(t *testing.T) {
	// private is deliberately synthetic sensitive-looking text used to prove that canonical diagnostic errors discard the original cause.
	// private — намеренно синтетический текст, похожий на чувствительные данные, для доказательства удаления исходной причины канонической диагностикой.
	const private = "token=PRIVATE_DIAGNOSTIC_SENTINEL user@example.test SELECT secret"
	for _, test := range []struct {
		input               Diagnostic
		stage, class, state string
		attempt             int
	}{
		{Diagnostic{Stage: StageAudit, Class: ClassPostgres, SQLState: "40001", Attempt: 3, RetryExhausted: true}, StageAudit, ClassPostgres, "40001", 3},
		{Diagnostic{Stage: StageAudit, Class: ClassPostgres, SQLState: "40001", Attempt: MaximumAttempts, RetryExhausted: true}, StageAudit, ClassPostgres, "40001", MaximumAttempts},
		{Diagnostic{Stage: StageAudit, Class: ClassPostgres, SQLState: "40001", Attempt: MaximumAttempts + 1}, StageAudit, ClassPostgres, "40001", 0},
		{Diagnostic{Stage: StageAudit, Class: ClassPostgres, SQLState: private, Attempt: 999}, StageAudit, ClassPostgres, "OTHER", 0},
		{Diagnostic{Stage: private, Class: private, SQLState: private, Attempt: -1}, StageUnknown, ClassUnknown, "NONE", 0},
		{Diagnostic{Stage: StageStorage, Class: ClassStorage, SQLState: "40001", Attempt: 1}, StageStorage, ClassStorage, "NONE", 1},
	} {
		raw := fmt.Errorf(private+": %w", errs.ErrUnavailable)
		err := WithFailure(raw, test.input)
		actual, ok := From(err)
		if !ok || actual.Stage != test.stage || actual.Class != test.class || actual.SQLState != test.state || actual.Attempt != test.attempt || actual.RetryExhausted != test.input.RetryExhausted {
			t.Fatal("diagnostic projection is not closed")
		}
		if !errors.Is(err, errs.ErrUnavailable) || errors.Is(err, raw) || strings.Contains(err.Error(), private) {
			t.Fatal("domain mapping changed or raw cause retained")
		}
	}
}

// TestArtifactDownloadDiagnosticKeepsExpectedDenialAndRejectsUntypedError checks that expected denials/nil retain identity, forged diagnostic-looking text gains no typed authority and bounded attempt metadata preserves the domain outcome.
// TestArtifactDownloadDiagnosticKeepsExpectedDenialAndRejectsUntypedError проверяет неизменность ожидаемых отказов/nil, отсутствие типизированных полномочий у похожего на диагностику текста и сохранение доменного результата при добавлении ограниченной попытки.
func TestArtifactDownloadDiagnosticKeepsExpectedDenialAndRejectsUntypedError(t *testing.T) {
	for _, outcome := range []error{errs.ErrForbidden, errs.ErrNotFound, errs.ErrVersionMismatch, errs.ErrInvalid, nil} {
		if WithFailure(outcome, NewDiagnostic(StageSelect, ClassPostgres, "40001")) != outcome {
			t.Fatal("expected domain refusal was changed")
		}
	}
	if _, ok := From(errors.New("safe_stage=AUDIT_INSERT sqlstate=40001")); ok {
		t.Fatal("error text acquired typed diagnostic authority")
	}
	err := WithFailure(errs.ErrConflict, NewDiagnostic(StageCommit, ClassPostgres, "40001"))
	err = WithAttempt(err, MaximumAttempts, true)
	diagnostic, ok := From(err)
	if !ok || !errors.Is(err, errs.ErrConflict) || diagnostic.Attempt != MaximumAttempts || !diagnostic.RetryExhausted {
		t.Fatal("attempt annotation changed exhausted outcome")
	}
}
