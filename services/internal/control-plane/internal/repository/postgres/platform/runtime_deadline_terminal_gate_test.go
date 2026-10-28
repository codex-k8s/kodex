package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

// terminalDeadlineRow returns only the controlled fixture projection or its storage failure.
// terminalDeadlineRow возвращает лишь управляемую проекцию оснастки либо её storage failure.
type terminalDeadlineRow func(...any) error

// Scan exposes a row without connecting to PostgreSQL or manufacturing a successful effect.
// Scan раскрывает строку без подключения к PostgreSQL либо создания успешного эффекта.
func (row terminalDeadlineRow) Scan(dest ...any) error { return row(dest...) }

// terminalDeadlineRows supplies one immutable elapsed clock to the real deadline reader.
// terminalDeadlineRows передаёт одни неизменные истёкшие часы настоящему deadline reader.
type terminalDeadlineRows struct {
	pgx.Rows
	read bool
}

// Next exposes exactly one clock and then terminates iteration.
// Next раскрывает ровно одни часы, затем завершает итерацию.
func (rows *terminalDeadlineRows) Next() bool {
	if rows.read {
		return false
	}
	rows.read = true
	return true
}

// Scan binds a valid expired owner clock, with no caller-controlled state or wall-clock mutation.
// Scan связывает допустимые истёкшие owner clocks без управляемого caller состояния или изменения времени.
func (rows *terminalDeadlineRows) Scan(dest ...any) error {
	started := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	*dest[0].(*string), *dest[1].(*string), *dest[2].(*string), *dest[3].(*string) = "run_terminal_clock", "wfv_terminal_clock", strings.Repeat("a", 64), ""
	*dest[4].(*int32), *dest[5].(*time.Time), *dest[6].(*time.Time), *dest[7].(*bool) = 1, started, started.Add(time.Second), true
	return nil
}

// Close releases no resources because this fixture never owns a network connection.
// Close не освобождает ресурсов, поскольку оснастка не владеет сетевым подключением.
func (rows *terminalDeadlineRows) Close() {}

// Err confirms the synthetic clock iteration completed without a storage failure.
// Err подтверждает завершение синтетической итерации без storage failure.
func (rows *terminalDeadlineRows) Err() error { return nil }

// terminalDeadlineTx admits only deadline reads; any business mutation or unrelated query fails the test.
// terminalDeadlineTx допускает лишь deadline reads; любая business mutation либо посторонний query роняет тест.
type terminalDeadlineTx struct {
	pgx.Tx
	t                        *testing.T
	gateState, rootState     string
	ownerError, recheckError error
	rechecks, clocks         int
	lateClose                bool
}

// QueryRow preserves exact tenant/gate pins and models the active-owner miss after terminal closure.
// QueryRow сохраняет точные tenant/gate pins и моделирует отсутствие active owner после terminal closure.
func (tx *terminalDeadlineTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	return terminalDeadlineRow(func(dest ...any) error {
		switch {
		case sql == queryOwnerGateExpiryLock:
			// Legacy fixture has no independent gate policy; expiry must not mutate it.
			// В прежней оснастке нет отдельного срока Gate; новый путь её не меняет.
			return pgx.ErrNoRows
		case sql == queryRuntimeDeadlineGate:
			*dest[0].(*string) = "owner-run-id"
			return nil
		case sql == queryRuntimeDeadlineOwner:
			return tx.ownerError
		case strings.Contains(sql, "name: runtime_deadline__terminal_gate"):
			tx.rechecks++
			pins, ok := args[0].(pgx.StrictNamedArgs)
			if !ok || pins["organization_id"] != "owner-tenant" || pins["gate_ref"] != "gat_terminal_clock" {
				tx.t.Fatal("terminal recheck lost owner pins")
			}
			if tx.lateClose && tx.rechecks == 1 || tx.gateState == "OPEN" {
				return pgx.ErrNoRows
			}
			if tx.recheckError != nil {
				return tx.recheckError
			}
			*dest[0].(*string) = tx.gateState
			return nil
		default:
			tx.t.Fatal("deadline miss reached an unrelated read or effect")
			return errors.New("unexpected fixture query")
		}
	})
}

// Query supplies only the owner clock and rejects any attempt to terminalize an already closed graph.
// Query передаёт лишь owner clock и запрещает попытку повторного terminal-перехода закрытого графа.
func (tx *terminalDeadlineTx) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	if sql != queryRuntimeDeadlineRead {
		tx.t.Fatal("terminal gate classification attempted a graph mutation")
	}
	tx.clocks++
	return &terminalDeadlineRows{}, nil
}

// TestRuntimeDeadlineTerminalGate keeps closed expired gate decisions nonretryable while preserving
// real storage failures and unexpected/open state as unavailable; the owner pipeline must not skip them.
// TestRuntimeDeadlineTerminalGate сохраняет окончательный отказ для закрытого истёкшего Gate,
// оставляя реальные storage failures и неожиданный/open state недоступными без пропуска owner pipeline.
func TestRuntimeDeadlineTerminalGate(t *testing.T) {
	storage := errors.New("synthetic storage failure")
	for _, test := range []struct {
		name, gate, root     string
		owner, recheck, want error
		checks, clocks       int
		lateClose            bool
	}{
		{"timeout", "CANCELLED", "FAILED", pgx.ErrNoRows, nil, errs.ErrAlreadyResolved, 1, 0, false},
		{"approved", "APPROVED", "SUCCEEDED", pgx.ErrNoRows, nil, errs.ErrAlreadyResolved, 1, 0, false},
		{"rejected", "REJECTED", "FAILED", pgx.ErrNoRows, nil, errs.ErrAlreadyResolved, 1, 0, false},
		{"changes", "CHANGES_REQUESTED", "FAILED", pgx.ErrNoRows, nil, errs.ErrAlreadyResolved, 1, 0, false},
		{"cancelled", "CANCELLED", "CANCELLED", pgx.ErrNoRows, nil, errs.ErrAlreadyResolved, 1, 0, false},
		{"owner-storage", "OPEN", "RUNNING", storage, nil, errs.ErrUnavailable, 1, 1, false},
		{"recheck-storage", "CANCELLED", "FAILED", pgx.ErrNoRows, storage, errs.ErrUnavailable, 2, 1, true},
		{"recheck-missing", "CANCELLED", "FAILED", pgx.ErrNoRows, pgx.ErrNoRows, errs.ErrUnavailable, 2, 1, true},
		{"open", "OPEN", "FAILED", pgx.ErrNoRows, nil, errs.ErrUnavailable, 2, 1, false},
		{"active-root-closed-child", "APPROVED", "RUNNING", pgx.ErrNoRows, nil, errs.ErrAlreadyResolved, 1, 0, false},
		{"unknown", "UNKNOWN", "FAILED", pgx.ErrNoRows, nil, errs.ErrUnavailable, 1, 0, false},
		{"concurrent-close", "CANCELLED", "FAILED", pgx.ErrNoRows, nil, errs.ErrAlreadyResolved, 2, 1, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &terminalDeadlineTx{t: t, gateState: test.gate, rootState: test.root, ownerError: test.owner, recheckError: test.recheck, lateClose: test.lateClose}
			var repository Repository
			result, handled, err := repository.executionDeadlineCommand(t.Context(), tx, scope{organizationID: "owner-tenant"}, command.Command{Payload: command.GateResolutionInput{GateRef: "gat_terminal_clock", Decision: "APPROVE"}})
			if !errors.Is(err, test.want) || handled || result.resourceRef != "" || tx.rechecks != test.checks || tx.clocks != test.clocks {
				t.Fatalf("terminal deadline classification differs: handled=%t checks=%d error=%v", handled, tx.rechecks, err)
			}
		})
	}
}
