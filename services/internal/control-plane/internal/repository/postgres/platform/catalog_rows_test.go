package platform

import (
	"errors"
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// TestReadCatalogRowsErrorPolicies protects nil empty results, row order, discarded partial scans
// and retained partial iteration results with the original error identity. Independent traces
// require Err only after exhausted Next and leave explicit Close with the caller.
// TestReadCatalogRowsErrorPolicies защищает nil пустого результата, порядок строк, отбрасывание
// partial Scan и сохранение partial iteration вместе с исходной ошибкой. Независимые traces
// требуют Err после завершения Next и оставляют явный Close вызывающему.
func TestReadCatalogRowsErrorPolicies(t *testing.T) {
	iterationFailure := errors.New("synthetic catalog iteration failure")
	for _, test := range []struct {
		name         string
		values       []int64
		fail         int
		iterationErr error
		want         []revisionFixtureValue
		wantErr      error
		trace        []string
	}{
		{"empty", nil, 0, nil, nil, nil, []string{"next", "err"}},
		{"ordered", []int64{4, -2}, 0, nil, []revisionFixtureValue{{4}, {-2}}, nil, []string{"next", "scan", "next", "scan", "next", "err"}},
		{"first-scan", []int64{4, -2}, 1, iterationFailure, nil, errs.ErrUnavailable, []string{"next", "scan"}},
		{"partial-scan", []int64{4, -2}, 2, iterationFailure, nil, errs.ErrUnavailable, []string{"next", "scan", "next", "scan"}},
		{"early-iteration", nil, 0, iterationFailure, nil, iterationFailure, []string{"next", "err"}},
		{"partial-iteration", []int64{4, -2}, 0, iterationFailure, []revisionFixtureValue{{4}, {-2}}, iterationFailure, []string{"next", "scan", "next", "scan", "next", "err"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			trace := []string{}
			rows := &revisionRowsFixture{values: test.values, fail: test.fail, iterationErr: test.iterationErr, trace: &trace}
			items, err := readCatalogRows(rows, scanRevisionFixture)
			if err != test.wantErr || !reflect.DeepEqual(items, test.want) || !reflect.DeepEqual(trace, test.trace) || rows.closed {
				t.Fatal("catalog row result, error or lifecycle policy changed")
			}
		})
	}
}

// closingRowsFixture observes Close in the same trace as Next, Scan and Err.
// closingRowsFixture отмечает Close в общей последовательности Next, Scan и Err.
type closingRowsFixture struct{ *revisionRowsFixture }

// Close records the caller-required cleanup before marking rows closed.
// Close записывает обязательную очистку перед отметкой закрытых rows.
func (rows *closingRowsFixture) Close() {
	*rows.trace = append(*rows.trace, "close")
	rows.revisionRowsFixture.Close()
}

// TestCollectClosedRows checks explicit cleanup, ordered rows and fail-closed partial results.
// TestCollectClosedRows проверяет явную очистку, порядок строк и закрытый отказ partial results.
func TestCollectClosedRows(t *testing.T) {
	for _, tc := range []struct {
		name         string
		values       []int64
		fail         int
		iterationErr error
		want         []revisionFixtureValue
		wantErr      error
		trace        []string
	}{
		{"empty", nil, 0, nil, []revisionFixtureValue{}, nil, []string{"next", "close", "err"}},
		{"success", []int64{4, 7}, 0, nil, []revisionFixtureValue{{4}, {7}}, nil, []string{"next", "scan", "next", "scan", "next", "close", "err"}},
		{"scan failure", []int64{4, 7}, 2, nil, nil, errs.ErrUnavailable, []string{"next", "scan", "next", "scan", "close"}},
		{"iteration failure", []int64{4}, 0, errors.New("synthetic iteration failure"), nil, errs.ErrUnavailable, []string{"next", "scan", "next", "close", "err"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			trace := []string{}
			rows := &closingRowsFixture{&revisionRowsFixture{values: tc.values, fail: tc.fail, iterationErr: tc.iterationErr, trace: &trace}}
			items, err := collectClosedRows(rows, make([]revisionFixtureValue, 0, 3), scanRevisionFixture)
			if !errors.Is(err, tc.wantErr) || !reflect.DeepEqual(items, tc.want) || !reflect.DeepEqual(trace, tc.trace) || !rows.closed {
				t.Fatal("closed row result, failure or operation order changed")
			}
		})
	}
}
