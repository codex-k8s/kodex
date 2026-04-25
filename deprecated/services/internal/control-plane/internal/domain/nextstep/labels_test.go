package nextstep

import "testing"

func TestNextMainPathStageSkipsDocAudit(t *testing.T) {
	t.Parallel()

	next, ok := NextMainPathStage("dev")
	if !ok {
		t.Fatal("NextMainPathStage(dev) ok = false, want true")
	}
	if got, want := next, "qa"; got != want {
		t.Fatalf("NextMainPathStage(dev) = %q, want %q", got, want)
	}
}

func TestNextMainPathRunLabelUsesConfiguredLabels(t *testing.T) {
	t.Parallel()

	labels := NewLabels(Config{
		RunDev: "stage:build",
		RunQA:  "stage:verify",
	})

	next, ok := labels.NextMainPathRunLabel("stage:build")
	if !ok {
		t.Fatal("NextMainPathRunLabel(stage:build) ok = false, want true")
	}
	if got, want := next, "stage:verify"; got != want {
		t.Fatalf("NextMainPathRunLabel(stage:build) = %q, want %q", got, want)
	}
}
