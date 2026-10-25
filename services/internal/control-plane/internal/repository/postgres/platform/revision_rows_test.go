package platform

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// revisionRowsFixture exposes only Next/Scan/Err/Close over independent synthetic revisions.
// Scan can populate a destination and still fail; every operation records its exact position.
// Other pgx methods remain unavailable so unexpected driver operations fail the test.
// revisionRowsFixture предоставляет только Next/Scan/Err/Close над отдельными synthetic revisions.
// Scan может заполнить destination и завершиться ошибкой; каждая операция записывает позицию.
// Остальные pgx methods недоступны, поэтому неожиданные driver operations приводят к отказу теста.
type revisionRowsFixture struct {
	pgx.Rows
	values       []int64
	index, fail  int
	iterationErr error
	trace        *[]string
	closed       bool
}

// Next advances one synthetic row and records exhaustion without implicitly calling Err or Close.
// Next продвигает одну synthetic row и записывает конец без неявных вызовов Err либо Close.
func (rows *revisionRowsFixture) Next() bool {
	*rows.trace = append(*rows.trace, "next")
	if rows.index == len(rows.values) {
		return false
	}
	rows.index++
	return true
}

// Scan writes the current revision before an optional failure to expose partial-result leaks.
// Scan записывает текущую revision до необязательной ошибки, выявляя утечку partial result.
func (rows *revisionRowsFixture) Scan(destinations ...any) error {
	*rows.trace = append(*rows.trace, "scan")
	if len(destinations) != 1 {
		panic("unexpected revision fixture destinations")
	}
	destination, ok := destinations[0].(*int64)
	if !ok || rows.index < 1 || rows.index > len(rows.values) {
		panic("unexpected revision fixture scan")
	}
	*destination = rows.values[rows.index-1]
	if rows.fail == rows.index {
		return errors.New("synthetic partial scan failure")
	}
	return nil
}

// Err records the iteration-error boundary independently of decoding or transaction completion.
// Err записывает границу iteration error отдельно от decoding и завершения transaction.
func (rows *revisionRowsFixture) Err() error {
	*rows.trace = append(*rows.trace, "err")
	return rows.iterationErr
}

// Close records an unexpected transfer of caller-owned row cleanup to the page helper.
// Close отмечает неожиданную передачу принадлежащего caller row cleanup в page helper.
func (rows *revisionRowsFixture) Close() { rows.closed = true }

// revisionPageTxFixture permits only observed Commit calls with an injected fixed outcome.
// Its embedded transaction has no other implementation and cannot silently issue SQL or rollback.
// revisionPageTxFixture допускает только наблюдаемые Commit с заданным исходом. Встроенная
// transaction не имеет других реализаций и не может незаметно выполнить SQL либо rollback.
type revisionPageTxFixture struct {
	pgx.Tx
	trace       *[]string
	lastContext context.Context
	calls       int
	err         error
}

// Commit records the exact caller context and call count without changing persistent state.
// Commit записывает точный caller context и число вызовов без изменения persistent state.
func (tx *revisionPageTxFixture) Commit(ctx context.Context) error {
	*tx.trace = append(*tx.trace, "commit")
	tx.lastContext, tx.calls = ctx, tx.calls+1
	return tx.err
}

// revisionFixtureValue carries one independently supplied signed revision for page-engine tests.
// revisionFixtureValue хранит отдельную signed revision для тестов page engine.
type revisionFixtureValue struct{ revision int64 }

// scanRevisionFixture returns both a populated item and the synthetic driver error when present.
// scanRevisionFixture возвращает заполненный item вместе с synthetic driver error при её наличии.
func scanRevisionFixture(row rowScanner) (revisionFixtureValue, error) {
	var item revisionFixtureValue
	err := row.Scan(&item.revision)
	return item, err
}

// TestRevisionRowsPageOrderAndFailures checks lookahead/cursor/Err/Commit ordering, empty slice
// presence and partial-result discard. No real PostgreSQL or authorization context is fabricated.
// TestRevisionRowsPageOrderAndFailures проверяет порядок lookahead/cursor/Err/Commit, presence
// пустого slice и отбрасывание partial result. Настоящие PostgreSQL и authority context не создаёт.
func TestRevisionRowsPageOrderAndFailures(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name                    string
		values, want            []int64
		fail                    int
		iterationErr, commitErr error
		next                    string
		trace                   []string
		commits                 int
	}{
		{"empty", nil, []int64{}, 0, nil, nil, "", []string{"next", "err", "commit"}, 1},
		{"one", []int64{8}, []int64{8}, 0, nil, nil, "", []string{"next", "scan", "next", "err", "commit"}, 1},
		{"full", []int64{8, 7}, []int64{8, 7}, 0, nil, nil, "", []string{"next", "scan", "next", "scan", "next", "err", "commit"}, 1},
		{"lookahead", []int64{8, 7, 6}, []int64{8, 7}, 0, nil, nil, "7", []string{"next", "scan", "next", "scan", "next", "scan", "next", "cursor", "err", "commit"}, 1},
		{"scan_first", []int64{8, 7}, nil, 1, nil, nil, "", []string{"next", "scan"}, 0},
		{"scan_second", []int64{8, 7}, nil, 2, nil, nil, "", []string{"next", "scan", "next", "scan"}, 0},
		{"iteration", []int64{8}, nil, 0, errors.New("synthetic iteration failure"), nil, "", []string{"next", "scan", "next", "err"}, 0},
		{"lookahead_iteration", []int64{8, 7, 6}, nil, 0, errors.New("synthetic iteration failure"), nil, "", []string{"next", "scan", "next", "scan", "next", "scan", "next", "cursor", "err"}, 0},
		{"commit", []int64{8}, nil, 0, nil, errors.New("synthetic commit failure"), "", []string{"next", "scan", "next", "err", "commit"}, 1},
		{"lookahead_commit", []int64{8, 7, 6}, nil, 0, nil, errors.New("synthetic commit failure"), "", []string{"next", "scan", "next", "scan", "next", "scan", "next", "cursor", "err", "commit"}, 1},
		{"cancelled_commit", nil, nil, 0, nil, context.Canceled, "", []string{"next", "err", "commit"}, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var trace []string
			rows := &revisionRowsFixture{values: test.values, fail: test.fail, iterationErr: test.iterationErr, trace: &trace}
			tx := &revisionPageTxFixture{trace: &trace, err: test.commitErr}
			ctx := t.Context()
			items, next, err := readRevisionRowsPage(ctx, tx, rows, 2, scanRevisionFixture, func(item revisionFixtureValue) string {
				trace = append(trace, "cursor")
				if item.revision != 7 {
					t.Fatal("cursor used lookahead instead of last selected revision")
				}
				return strconv.FormatInt(item.revision, 10)
			})
			failed := test.fail != 0 || test.iterationErr != nil || test.commitErr != nil
			if failed {
				if err != errs.ErrUnavailable || items != nil || next != "" {
					t.Fatal("failed page leaked partial values or error")
				}
			} else {
				if err != nil || items == nil || cap(items) != 3 {
					t.Fatal("success error, empty presence or capacity changed")
				}
				values := make([]int64, len(items))
				for i, item := range items {
					values[i] = item.revision
				}
				if !reflect.DeepEqual(values, test.want) || next != test.next {
					t.Fatal("selected revision order or cursor changed")
				}
			}
			if !reflect.DeepEqual(trace, test.trace) || tx.calls != test.commits || rows.closed {
				t.Fatal("row/error/cursor/commit order or cleanup ownership changed")
			}
			if tx.calls != 0 && tx.lastContext != ctx {
				t.Fatal("commit context changed")
			}
		})
	}
}

// TestRuntimeVersionRowsErrorBoundary preserves the older scanner's independent Err check, all
// lookahead rows and absence of transaction/cleanup effects while sharing only row collection.
// TestRuntimeVersionRowsErrorBoundary сохраняет отдельный Err check прежнего scanner, все
// lookahead rows и отсутствие transaction/cleanup effects, разделяя только сбор строк.
func TestRuntimeVersionRowsErrorBoundary(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name         string
		values       []int64
		fail         int
		iterationErr error
		trace        []string
	}{
		{"empty", nil, 0, nil, []string{"next", "err"}},
		{"lookahead_untrimmed", []int64{8, 7, 6}, 0, nil, []string{"next", "scan", "next", "scan", "next", "scan", "next", "err"}},
		{"partial_scan", []int64{8, 7}, 2, nil, []string{"next", "scan", "next", "scan"}},
		{"iteration", []int64{8}, 0, errors.New("synthetic iteration failure"), []string{"next", "scan", "next", "err"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var trace []string
			rows := &revisionRowsFixture{values: test.values, fail: test.fail, iterationErr: test.iterationErr, trace: &trace}
			items, err := scanRuntimeVersionRows(rows, 2, scanRevisionFixture)
			if test.fail != 0 || test.iterationErr != nil {
				if items != nil || err != errs.ErrUnavailable {
					t.Fatal("runtime scanner exposed partial items")
				}
			} else if err != nil || items == nil || len(items) != len(test.values) || cap(items) != 3 {
				t.Fatal("runtime scanner changed rows or empty presence")
			}
			if !reflect.DeepEqual(trace, test.trace) || rows.closed {
				t.Fatal("runtime iteration boundary or cleanup ownership changed")
			}
		})
	}
}
