package grpc

import (
	"math"
	"testing"
)

func TestMapInt64AcceptsUnsignedValuesAndRejectsOverflow(t *testing.T) {
	t.Parallel()

	values := map[string]any{
		"uint32":   uint32(7),
		"uint64":   uint64(11),
		"uint":     uint(13),
		"overflow": uint64(math.MaxInt64) + 1,
	}
	for key, expected := range map[string]int64{
		"uint32": 7, "uint64": 11, "uint": 13, "overflow": 0,
	} {
		if actual := mapInt64(values, key); actual != expected {
			t.Fatalf("%s: получено %d, ожидалось %d", key, actual, expected)
		}
	}
}

func TestCastRuntimeRevisionPreservesUnsignedContractRevision(t *testing.T) {
	t.Parallel()

	revision := castRuntimeRevision(map[string]any{
		"roleRuntimeContractRevision": uint64(3),
	})
	if revision.GetRoleRuntimeContractRevision() != 3 {
		t.Fatalf("revision runtime-контракта потеряна: %d", revision.GetRoleRuntimeContractRevision())
	}
}

func TestCastClaimPreservesAuthoritativeProjectBinding(t *testing.T) {
	t.Parallel()

	claim := castClaim(map[string]any{
		"runRef":     "run_abcdefgh",
		"projectRef": "prj_abcdefgh",
		"sessionRef": "ses_abcdefgh",
	})
	if claim.GetRun().GetProjectRef() != "prj_abcdefgh" {
		t.Fatalf("project binding потерян: %q", claim.GetRun().GetProjectRef())
	}
}
