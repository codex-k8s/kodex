package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// TestWorkflowEffectExecutionTuple proves node identity alone cannot turn a fresh execution into an original RPC replay.
// TestWorkflowEffectExecutionTuple доказывает, что node identity не подменяет fresh execution исходным RPC replay.
func TestWorkflowEffectExecutionTuple(t *testing.T) {
	original := workflowEffectIntent{OriginNodeID: "node", OriginRunID: "run", OriginRevisionID: "revision", OriginLeaseID: "lease", OriginGeneration: 1, OriginAttempt: 1, OriginBusiness: 1}
	for _, change := range []func(*workflowEffectIntent){func(v *workflowEffectIntent) { v.OriginNodeID = "other" }, func(v *workflowEffectIntent) { v.OriginRunID = "other" }, func(v *workflowEffectIntent) { v.OriginRevisionID = "other" }, func(v *workflowEffectIntent) { v.OriginLeaseID = "other" }, func(v *workflowEffectIntent) { v.OriginGeneration++ }, func(v *workflowEffectIntent) { v.OriginAttempt++ }, func(v *workflowEffectIntent) { v.OriginBusiness++ }} {
		altered := original
		change(&altered)
		if sameWorkflowEffectExecution(original, altered) {
			t.Fatal("fresh immutable execution was treated as the original replay")
		}
	}
	if !sameWorkflowEffectExecution(original, original) {
		t.Fatal("exact original execution was lost")
	}
}

// workflowEffectReadTx observes the reader's fixed query order; SQL semantics are exercised separately by native expiry/CONTINUES cases.
// workflowEffectReadTx наблюдает fixed query order reader; SQL semantics отдельно проверяются native expiry/CONTINUES cases.
type workflowEffectReadTx struct {
	pgx.Tx
	bridges     int
	bridgeError error
	legacyCalls int
}

// Query distinguishes exact bridge read from unchanged legacy read and never executes arbitrary SQL.
// Query различает exact bridge read и unchanged legacy read, не выполняя произвольный SQL.
func (tx *workflowEffectReadTx) Query(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
	if query == queryWorkflowEffectResult {
		return &workflowEffectReadRows{total: tx.bridges}, tx.bridgeError
	}
	if query == queryWorkflowResultIntegration {
		tx.legacyCalls++
		return &workflowEffectReadRows{total: 1, failure: workflowReceiptLineageAmbiguous}, nil
	}
	return nil, errs.ErrUnavailable
}

// workflowEffectReadRows supplies only closed owner-result columns to the reader-order regression.
// workflowEffectReadRows задаёт только closed owner-result columns для reader-order regression.
type workflowEffectReadRows struct {
	pgx.Rows
	total, index int
	failure      string
}

// Next advances one synthetic result row without inventing receipt authority.
// Next переводит synthetic result row, не создавая receipt authority.
func (rows *workflowEffectReadRows) Next() bool { rows.index++; return rows.index <= rows.total }

// Scan supplies exactly the existing seven-column result contract.
// Scan задаёт ровно существующий seven-column result contract.
func (rows *workflowEffectReadRows) Scan(dest ...any) error {
	if len(dest) != 7 {
		return errs.ErrUnavailable
	}
	*dest[0].(*string) = "github.pull_request.create"
	*dest[1].(*[]byte) = []byte(`{}`)
	*dest[2].(*[]byte) = []byte(`{}`)
	*dest[3].(*string) = "{}"
	*dest[4].(*string) = "digest"
	*dest[5].(*string) = "provider"
	*dest[6].(*string) = rows.failure
	return nil
}

// Err reports no cursor failure in the deterministic reader-order fixture.
// Err сообщает отсутствие cursor failure в deterministic fixture.
func (rows *workflowEffectReadRows) Err() error { return nil }

// Close releases no external resource in this in-memory query-order proof.
// Close не освобождает внешних ресурсов в in-memory proof.
func (rows *workflowEffectReadRows) Close() {}

// TestWorkflowEffectReaderPrefersExactBridge verifies that only a proved unique bridge precedes legacy ambiguity; absence, ambiguity and SQL errors remain closed.
// TestWorkflowEffectReaderPrefersExactBridge проверяет priority только proved unique bridge; отсутствие, ambiguity и SQL errors остаются закрытыми.
func TestWorkflowEffectReaderPrefersExactBridge(t *testing.T) {
	for _, test := range []struct {
		name        string
		count       int
		queryError  error
		reason      string
		wantError   bool
		legacyCalls int
	}{
		{"unique-bridge", 1, nil, "", false, 0}, {"no-bridge-keeps-legacy-ambiguity", 0, nil, workflowReceiptLineageAmbiguous, false, 1}, {"multiple-distinct-intents", 2, nil, workflowReceiptLineageAmbiguous, false, 0}, {"bridge-sql-failure", 0, errs.ErrUnavailable, "", true, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &workflowEffectReadTx{bridges: test.count, bridgeError: test.queryError}
			_, reason, err := readWorkflowIntegrationResultTx(t.Context(), tx, scope{}, map[string]any{}, "erc_ownerfixture")
			if reason != test.reason || errors.Is(err, errs.ErrUnavailable) != test.wantError || tx.legacyCalls != test.legacyCalls {
				t.Fatal("exact bridge priority weakened a closed legacy or error boundary")
			}
		})
	}
}
