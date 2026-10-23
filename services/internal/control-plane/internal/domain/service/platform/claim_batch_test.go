package platform

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// TestValidatePlatformClaimBatchPreservesBounds checks both inclusive batch boundaries, nearby and extreme int32 values, Unicode blank instances and the prior acceptance of nonblank untrimmed or NUL-containing instance strings. This input-only unit does not acquire a lease, validate owner authority or invoke a repository.
// TestValidatePlatformClaimBatchPreservesBounds проверяет обе включённые границы пачки, соседние и крайние int32, экземпляры из Unicode-пробелов и прежний приём непустых строк с внешними пробелами или NUL. Эта проверка только входа не захватывает lease, не проверяет полномочия владельца и не вызывает репозиторий.
func TestValidatePlatformClaimBatchPreservesBounds(t *testing.T) {
	for _, test := range []struct {
		name, instance string
		limit          int32
		invalid        bool
	}{
		{name: "empty instance", limit: 1, invalid: true},
		{name: "ascii spaces", instance: " \t\n", limit: 1, invalid: true},
		{name: "unicode spaces", instance: "\u2003\u00a0", limit: 1, invalid: true},
		{name: "lower bound", instance: "worker", limit: 1},
		{name: "upper bound", instance: "worker", limit: 32},
		{name: "zero limit", instance: "worker", invalid: true},
		{name: "negative limit", instance: "worker", limit: -1, invalid: true},
		{name: "above upper bound", instance: "worker", limit: 33, invalid: true},
		{name: "int32 minimum", instance: "worker", limit: -1 << 31, invalid: true},
		{name: "int32 maximum", instance: "worker", limit: 1<<31 - 1, invalid: true},
		{name: "outer whitespace accepted", instance: " worker ", limit: 2},
		{name: "unicode instance", instance: "исполнитель", limit: 3},
		{name: "nul unchanged", instance: "\x00", limit: 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validatePlatformClaimBatch(test.instance, test.limit)
			if test.invalid {
				if !errors.Is(err, errs.ErrInvalid) {
					t.Fatal("claim-batch rejection boundary changed")
				}
			} else if err != nil {
				t.Fatal("claim-batch accepted range changed")
			}
		})
	}
}
