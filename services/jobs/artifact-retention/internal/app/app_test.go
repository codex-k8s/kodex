package app

import (
	"errors"
	"testing"
)

func TestShouldResetRetentionBackoff(t *testing.T) {
	tests := []struct {
		name      string
		processed int
		err       error
		want      bool
	}{
		{name: "idle", want: false},
		{name: "processed", processed: 1, want: true},
		{name: "failed", err: errors.New("retention failed"), want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldResetRetentionBackoff(test.processed, test.err); got != test.want {
				t.Fatalf("shouldResetRetentionBackoff() = %v, want %v", got, test.want)
			}
		})
	}
}
