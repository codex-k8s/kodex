package platform

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// TestOverviewCounters proves full totals survive int32 conversion and rejects negative or
// overflowing values in every position without exposing a partially populated overview.
// TestOverviewCounters доказывает сохранение full totals при int32 conversion и закрыто отклоняет
// отрицательное значение либо переполнение каждой позиции без частично заполненного обзора.
func TestOverviewCounters(t *testing.T) {
	for _, counts := range [][4]int64{{0, 0, 0, 0}, {3, 24, 29, 37}, {1<<31 - 1, 1<<31 - 1, 1<<31 - 1, 1<<31 - 1}} {
		value, err := overviewCounters(counts[0], counts[1], counts[2], counts[3])
		if err != nil || int64(value.ProjectCount) != counts[0] || int64(value.AgentCount) != counts[1] || int64(value.ActiveRunCount) != counts[2] || int64(value.PendingGateCount) != counts[3] {
			t.Fatal("valid full overview totals changed")
		}
	}
	for index := range 4 {
		for _, bad := range []int64{-1, 1 << 31, 1 << 53} {
			counts := [4]int64{1, 2, 3, 4}
			counts[index] = bad
			value, err := overviewCounters(counts[0], counts[1], counts[2], counts[3])
			if !errors.Is(err, errs.ErrUnavailable) || value.ProjectCount != 0 || value.AgentCount != 0 || value.ActiveRunCount != 0 || value.PendingGateCount != 0 {
				t.Fatal("invalid total produced a partial or overflowing overview")
			}
		}
	}
}
