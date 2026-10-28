package platform

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// workflowEvidenceClaimDiagnostics observes at most two closed eligibility failures in a fixture.
// It stores no task, SQL, secret, locator, raw error or production state and cannot alter a claim.
// workflowEvidenceClaimDiagnostics наблюдает максимум два закрытых отказа доступности в fixture.
// Task, SQL, secret, locator, raw error и production state не сохраняются; claim не меняется.
type workflowEvidenceClaimDiagnostics struct {
	mu      sync.Mutex
	records [2]string
	count   int
}

// Enabled admits warnings only during the exact synchronous fixture claim.
// Enabled допускает только предупреждения во время точного синхронного fixture claim.
func (*workflowEvidenceClaimDiagnostics) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelWarn
}

// Handle keeps only the existing eligibility message and three independently closed attributes.
// Unknown attributes and values never become diagnostic payload; memory remains bounded.
// Handle сохраняет только существующее eligibility message и три закрытых атрибута.
// Неизвестные поля/значения не попадают в вывод; память ограничена.
func (trace *workflowEvidenceClaimDiagnostics) Handle(_ context.Context, record slog.Record) error {
	if record.Message != runtimeCandidateEligibilityDiagnosticMessage {
		return nil
	}
	stage, class, reason := runtimeEligibilityUnknownDiagnostic, runtimeEligibilityUnknownDiagnostic, runtimeEligibilityUnknownDiagnostic
	record.Attrs(func(a slog.Attr) bool {
		if a.Value.Kind() != slog.KindString {
			return true
		}
		value := a.Value.String()
		switch a.Key {
		case "safe_stage":
			switch value {
			case "provider_profile", "runtime_catalog", "session_catalog", "execution_input", "runtime_environment", "assistant_context", "continuation", "prompt_authority", "prompt_context", "prompt_materialization", "managed_mcp", "runtime_context", "file_catalog", "runtime_revision", "session_storage":
				stage = value
			}
		case "error_class":
			switch value {
			case "CONFLICT", "VERSION_MISMATCH", "NOT_FOUND", "FORBIDDEN", "CAPABILITY_REQUIRED", "INVALID", "TERMINAL_STORAGE":
				class = value
			}
		case runtimeEligibilityReasonAttribute:
			reason = runtimeEligibilityReason(runtimeEligibilityDiagnostic(value, errs.ErrConflict))
		}
		return true
	})
	trace.mu.Lock()
	defer trace.mu.Unlock()
	if trace.count < len(trace.records) {
		trace.records[trace.count] = fmt.Sprintf("stage=%s class=%s reason=%s", stage, class, reason)
		trace.count++
	}
	return nil
}

// WithAttrs deliberately drops inherited context instead of retaining unreviewed fixture payload.
// WithAttrs намеренно не сохраняет унаследованный контекст с непроверенными payload.
func (trace *workflowEvidenceClaimDiagnostics) WithAttrs([]slog.Attr) slog.Handler { return trace }

// WithGroup retains the same bounded observer without collecting arbitrary group names.
// WithGroup сохраняет ограниченный observer, не собирая произвольные имена групп.
func (trace *workflowEvidenceClaimDiagnostics) WithGroup(string) slog.Handler { return trace }

// summary returns only closed metadata after the synchronous claim and never changes owner state.
// summary возвращает только закрытые metadata после claim и не меняет owner state.
func (trace *workflowEvidenceClaimDiagnostics) summary() string {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	return strings.Join(trace.records[:trace.count], "; ")
}

// TestWorkflowEvidenceClaimDiagnosticIsClosed prevents the test observer from disclosing private
// errors or accepting unbounded producer values while preserving the exact useful stage.
// TestWorkflowEvidenceClaimDiagnosticIsClosed запрещает observer раскрывать private errors и
// произвольные producer values, сохраняя точный полезный stage.
func TestWorkflowEvidenceClaimDiagnosticIsClosed(t *testing.T) {
	trace := &workflowEvidenceClaimDiagnostics{}
	for index := 0; index < 3; index++ {
		record := slog.NewRecord(time.Time{}, slog.LevelWarn, runtimeCandidateEligibilityDiagnosticMessage, 0)
		record.AddAttrs(slog.String("safe_stage", "prompt_context"), slog.String("error_class", "CONFLICT"), slog.String(runtimeEligibilityReasonAttribute, "private error text"), slog.String("raw_sql", "private SQL"), slog.String("secret", "private secret"))
		if err := trace.Handle(t.Context(), record); err != nil {
			t.Fatal("claim diagnostic failed")
		}
	}
	value := trace.summary()
	if trace.count != 2 || strings.Contains(value, "private") || strings.Count(value, "stage=prompt_context class=CONFLICT reason=UNKNOWN") != 2 {
		t.Fatal("claim diagnostic is not closed or bounded")
	}
}
