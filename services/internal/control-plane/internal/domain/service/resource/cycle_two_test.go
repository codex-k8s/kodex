package resource

import "testing"

func TestWeightedCandidateIndexExactCycle(t *testing.T) {
	t.Parallel()

	weights := []uint32{1, 2}
	want := []int{0, 1, 1}
	for slot, expected := range want {
		actual, ok := weightedCandidateIndex(weights, uint64(slot))
		if !ok || actual != expected {
			t.Fatalf("slot %d: got (%d, %t), want (%d, true)", slot, actual, ok, expected)
		}
	}
	if _, ok := weightedCandidateIndex(weights, uint64(len(want))); ok {
		t.Fatal("slot outside the exact cycle must fail closed")
	}
}

func TestWeightedCandidateIndexRejectsZeroWeight(t *testing.T) {
	t.Parallel()

	if _, ok := weightedCandidateIndex([]uint32{1, 0, 2}, 1); ok {
		t.Fatal("zero weight must fail closed")
	}
}
