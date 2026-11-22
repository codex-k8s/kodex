package platform

import (
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// TestKubernetesOutputDeadlineNeverWidensNativeCredit covers DB precision, the five-second cap and expiry.
// Exact microsecond normalization prevents a persisted deadline from differing from the broker wire receipt.
// TestKubernetesOutputDeadlineNeverWidensNativeCredit проверяет DB precision, five-second cap и expiry.
// Exact microsecond normalization сохраняет одинаковый persisted deadline и broker wire receipt.
func TestKubernetesOutputDeadlineNeverWidensNativeCredit(t *testing.T) {
	now := time.Unix(1800000000, 123456789)
	for _, test := range []struct{ native, ceiling, want time.Time }{
		{now.Add(time.Hour), now.Add(30 * time.Second), now.Truncate(time.Microsecond).Add(5 * time.Second)},
		{now.Add(2 * time.Second), now.Add(30 * time.Second), now.Add(2 * time.Second).Truncate(time.Microsecond)},
		{now.Add(4 * time.Second), now.Add(time.Second), now.Add(time.Second).Truncate(time.Microsecond)},
	} {
		got, err := kubernetesOutputFrameDeadline(test.native, now, test.ceiling)
		if err != nil || !got.Equal(test.want) || got.After(test.native) || got.Nanosecond()%1000 != 0 {
			t.Fatal("output native deadline widened or changed storage precision", got, err)
		}
	}
	for _, native := range []time.Time{{}, now.Add(-time.Second), now.Add(100 * time.Nanosecond)} {
		if _, err := kubernetesOutputFrameDeadline(native, now, now.Add(time.Hour)); !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("expired/non-persistable native witness became credit", err)
		}
	}
}
