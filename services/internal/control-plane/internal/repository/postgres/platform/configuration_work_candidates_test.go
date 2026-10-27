package platform

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// candidateRowsFixture records close and error checks for synthetic rows.
// candidateRowsFixture фиксирует порядок закрытия и чтения ошибок синтетических rows.
type candidateRowsFixture struct {
	pgx.Rows
	values        []string
	index         int
	scanFail      int
	iterationFail bool
	calls         []string
}

// Next reports whether another synthetic reference is available.
// Next сообщает о следующей синтетической ссылке.
func (r *candidateRowsFixture) Next() bool { return r.index < len(r.values) }

// Scan reads one reference or reproduces a driver error.
// Scan читает одну ссылку либо воспроизводит ошибку драйвера.
func (r *candidateRowsFixture) Scan(dest ...any) error {
	r.calls = append(r.calls, "scan")
	r.index++
	if r.index == r.scanFail {
		return errors.New("scan failed")
	}
	*dest[0].(*string) = r.values[r.index-1]
	return nil
}

// Close records that the rows were closed.
// Close фиксирует закрытие rows.
func (r *candidateRowsFixture) Close() { r.calls = append(r.calls, "close") }

// Err records the final error check after closing rows.
// Err фиксирует итоговую проверку после закрытия rows.
func (r *candidateRowsFixture) Err() error {
	r.calls = append(r.calls, "err")
	if r.iterationFail {
		return errors.New("iteration failed")
	}
	return nil
}

// candidateTxFixture checks the query and tenant boundary passed to the transaction.
// candidateTxFixture проверяет передачу запроса и границы tenant в транзакцию.
type candidateTxFixture struct {
	pgx.Tx
	rows      *candidateRowsFixture
	queryFail bool
	query     string
	args      []any
}

// Query records the query and returns synthetic rows.
// Query записывает запрос и возвращает синтетические rows.
func (tx *candidateTxFixture) Query(_ context.Context, query string, args ...any) (pgx.Rows, error) {
	tx.query, tx.args = query, args
	if tx.queryFail {
		return nil, errors.New("query failed")
	}
	return tx.rows, nil
}

// TestReadConfigurationWorkCandidates checks ordering, empty results and fail-closed behavior.
// TestReadConfigurationWorkCandidates проверяет порядок, пустой результат и закрытый отказ.
func TestReadConfigurationWorkCandidates(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		values                   []string
		scanFail                 int
		iterationFail, queryFail bool
		want                     []string
		calls                    []string
		fail                     bool
	}{
		{name: "empty", want: []string{}, calls: []string{"close", "err"}},
		{name: "ordered", values: []string{"one", "two"}, want: []string{"one", "two"}, calls: []string{"scan", "scan", "close", "err"}},
		{name: "query failure", queryFail: true, fail: true},
		{name: "partial scan failure", values: []string{"one", "two"}, scanFail: 2, calls: []string{"scan", "scan", "close"}, fail: true},
		{name: "iteration failure", values: []string{"one"}, iterationFail: true, calls: []string{"scan", "close", "err"}, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := &candidateRowsFixture{values: tc.values, scanFail: tc.scanFail, iterationFail: tc.iterationFail}
			tx := &candidateTxFixture{rows: rows, queryFail: tc.queryFail}
			got, err := readConfigurationWorkCandidates(context.Background(), tx, "fixed query", "tenant", int32(7))
			if tx.query != "fixed query" || !reflect.DeepEqual(tx.args, []any{"tenant", int32(7)}) {
				t.Fatal("query arguments changed")
			}
			if tc.fail {
				if !errors.Is(err, errs.ErrUnavailable) || got != nil {
					t.Fatal("expected unavailable without partial references")
				}
			} else if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("unexpected references: %v, %v", got, err)
			}
			if !reflect.DeepEqual(rows.calls, tc.calls) {
				t.Fatalf("unexpected row calls: %v", rows.calls)
			}
		})
	}
}
