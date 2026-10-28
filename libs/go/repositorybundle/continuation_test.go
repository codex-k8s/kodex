package repositorybundle

import "testing"

// TestContinuationDeliveryBoundary keeps execution-delivery failure independent from the immutable Git outcome and rejects partial/unknown diagnostic tuples.
// TestContinuationDeliveryBoundary отделяет failure execution delivery от immutable Git outcome и отклоняет partial/unknown diagnostic tuples.
func TestContinuationDeliveryBoundary(t *testing.T) {
	for _, v := range []struct {
		state, code, node string
		valid             bool
	}{
		{"", "", "", true}, {"BLOCKED", "CALLBACK_INPUT_TOO_LARGE", "nod_exact12345", true}, {"CANCELLED", "", "nod_exact12345", true},
		{"BLOCKED", "CONTINUATION_AUTHORITY_CHANGED", "nod_exact12345", true},
		{"", "CALLBACK_INPUT_TOO_LARGE", "", false}, {"BLOCKED", "", "nod_exact12345", false}, {"BLOCKED", "UNAVAILABLE", "nod_exact12345", false}, {"CANCELLED", "CALLBACK_INPUT_TOO_LARGE", "nod_exact12345", false}, {"BLOCKED", "CALLBACK_INPUT_TOO_LARGE", "nod_short", false}, {"BLOCKED", "CALLBACK_INPUT_TOO_LARGE", "nod_exact/12345", false}, {"RETRY", "", "nod_exact12345", false},
	} {
		if ValidContinuation(v.state, v.code, v.node) != v.valid {
			t.Fatalf("invalid continuation tuple classification: %q/%q", v.state, v.code)
		}
	}
}
