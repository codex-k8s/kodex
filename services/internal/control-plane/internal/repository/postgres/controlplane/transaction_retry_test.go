package controlplane

import (
	"testing"
	"time"
)

func TestTransactionRetryDelayIsBoundedExponential(t *testing.T) {
	want := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond, 80 * time.Millisecond}
	for attempt, expected := range want {
		if actual := transactionRetryDelay(attempt); actual != expected {
			t.Fatalf("attempt %d delay = %s, want %s", attempt, actual, expected)
		}
	}
}
