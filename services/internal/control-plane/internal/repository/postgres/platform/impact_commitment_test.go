package platform

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// TestImpactCommitmentDigestExactJSON checks fixed JSON digests, nil/empty distinction and closed failure.
// TestImpactCommitmentDigestExactJSON проверяет точные digest JSON, различие nil и empty и закрытый отказ.
func TestImpactCommitmentDigestExactJSON(t *testing.T) {
	// record supplies the synthetic typed Plan and Item used by the digest test.
	// record задаёт синтетические типизированные Plan и Item для проверки digest.
	type record struct{ Ref string }
	for _, tc := range []struct {
		name  string
		items []record
		want  string
	}{
		{"one", []record{{Ref: "i"}}, "be2d12585f63b95fce11e2bcd9690223a0764516b95c2768d42d2efa47266fb4"},
		{"nil", nil, "52b2106f26d8ba4a657cdb8eceae9ddf48a3ce88e91b270c2d8c8a89ba67a27c"},
		{"empty", []record{}, "453438221d6f41bed12c963070a847ef2d0e55a6d246929a24e6bd4dc1d485bc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := impactCommitmentDigest(record{Ref: "p"}, "a", tc.items)
			if err != nil || got != tc.want {
				t.Fatal("commitment encoding changed")
			}
		})
	}
	got, err := impactCommitmentDigest(make(chan int), "a", []record{})
	if got != "" || !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("marshal failure must return unavailable without digest")
	}
}
