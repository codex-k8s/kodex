package platform

import (
	"testing"
	"time"
)

func TestResumableSessionCursorOrdersNewestFirst(t *testing.T) {
	now := time.Date(2026, time.September, 27, 2, 0, 0, 0, time.UTC)
	cursor := resumableSessionCursor{Ref: "run_m", CreatedAt: now}
	for _, check := range []struct {
		name string
		item resumableSessionCandidate
		want bool
	}{
		{name: "older run", item: resumableSessionCandidate{RunRef: "run_z", CreatedAt: now.Add(-time.Second)}, want: true},
		{name: "same time lower ref", item: resumableSessionCandidate{RunRef: "run_a", CreatedAt: now}, want: true},
		{name: "same run", item: resumableSessionCandidate{RunRef: "run_m", CreatedAt: now}},
		{name: "same time higher ref", item: resumableSessionCandidate{RunRef: "run_z", CreatedAt: now}},
		{name: "newer run", item: resumableSessionCandidate{RunRef: "run_a", CreatedAt: now.Add(time.Second)}},
	} {
		t.Run(check.name, func(t *testing.T) {
			if got := resumableSessionAfterCursor(check.item, cursor); got != check.want {
				t.Fatalf("resumable session cursor order: got %v, want %v", got, check.want)
			}
		})
	}
	if !resumableSessionAfterCursor(resumableSessionCandidate{RunRef: "run_z", CreatedAt: now}, resumableSessionCursor{}) {
		t.Fatal("first page skipped a resumable session")
	}
}
