package runtimecontract

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func deadlineFixture() *RuntimeExecutionDeadline {
	start := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	clock := RuntimeExecutionClock{RunRef: "run_fixture01", WorkflowVersionRef: "wfv_fixture01", WorkflowVersionDigest: strings.Repeat("a", 64), TimeoutSeconds: 86400, StartedAt: start, DeadlineAt: start.Add(24 * time.Hour)}
	return &RuntimeExecutionDeadline{Policy: WorkflowExecutionDeadlinePolicy, EffectiveDeadlineAt: clock.DeadlineAt, Clocks: []RuntimeExecutionClock{clock}}
}

func TestExecutionDeadlineValidationAndImmutableDigest(t *testing.T) {
	d := deadlineFixture()
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, testcase := range []struct {
		name   string
		mutate func(*RuntimeExecutionDeadline)
	}{
		{"wrong_effective", func(d *RuntimeExecutionDeadline) { d.EffectiveDeadlineAt = d.EffectiveDeadlineAt.Add(time.Second) }},
		{"duplicate", func(d *RuntimeExecutionDeadline) { d.Clocks = append(d.Clocks, d.Clocks[0]) }},
		{"reset", func(d *RuntimeExecutionDeadline) { d.Clocks[0].StartedAt = d.Clocks[0].StartedAt.Add(time.Second) }},
		{"step_limit", func(d *RuntimeExecutionDeadline) {
			d.Clocks[0].StepKey = "step-001"
			d.Clocks[0].TimeoutSeconds = 86401
		}},
		{"unknown_policy", func(d *RuntimeExecutionDeadline) { d.Policy = "other" }},
		{"invalid_pin", func(d *RuntimeExecutionDeadline) { d.Clocks[0].WorkflowVersionDigest = "invalid" }},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			d := deadlineFixture()
			testcase.mutate(d)
			if d.Validate() == nil {
				t.Fatal("invalid deadline accepted")
			}
		})
	}
	input := validRunnerInputFixture()
	source := RuntimeRevisionCredentialSource{SecretName: "provider-auth", SecretUID: "uid-1", SecretResourceVersion: "19"}
	original, err := RuntimeRevisionDigest(input, source)
	if err != nil {
		t.Fatal(err)
	}
	input.ExecutionDeadline = deadlineFixture()
	pinned, err := RuntimeRevisionDigest(input, source)
	if err != nil || original == pinned {
		t.Fatalf("deadline not bound: %v", err)
	}
	for _, scope := range []AssistantScope{AssistantScopeSystem, AssistantScopeProject} {
		input.AssistantScope = scope
		if input.Validate() == nil {
			t.Fatal("assistant accepted workflow clock")
		}
	}
}

func TestExecutionDeadlineDoesNotUseControllerFallbackOrReset(t *testing.T) {
	d := deadlineFixture()
	d.Clocks[0].StartedAt = time.Now().UTC()
	d.Clocks[0].DeadlineAt = d.Clocks[0].StartedAt.Add(24 * time.Hour)
	d.EffectiveDeadlineAt = d.Clocks[0].DeadlineAt
	input := RunnerInput{ExecutionDeadline: d}
	for range 2 {
		ctx, cancel := input.BoundExecutionDeadline(t.Context(), time.Hour)
		got, _ := ctx.Deadline()
		cancel()
		if !got.Equal(d.EffectiveDeadlineAt) {
			t.Fatal("durable workflow deadline was reset or truncated")
		}
	}
	d.EffectiveDeadlineAt = time.Now().UTC().Add(-time.Second)
	ctx, cancel := input.BoundExecutionDeadline(t.Context(), time.Hour)
	defer cancel()
	if ctx.Err() != context.DeadlineExceeded {
		t.Fatal("expired deadline was accepted")
	}
	ordinary, cancel := (RunnerInput{}).BoundExecutionDeadline(t.Context(), time.Hour)
	defer cancel()
	got, _ := ordinary.Deadline()
	if time.Until(got) < 59*time.Minute {
		t.Fatal("ordinary fallback changed")
	}
}

func TestExecutionDeadlineClosedDecoderAndMinimum(t *testing.T) {
	deadline := deadlineFixture()
	clock := deadline.Clocks[0]
	clock.RunRef, clock.StepKey, clock.TimeoutSeconds = "run_stepclock01", "step-001", 1200
	clock.DeadlineAt = clock.StartedAt.Add(1200 * time.Second)
	deadline.Clocks = append(deadline.Clocks, clock)
	deadline.EffectiveDeadlineAt = clock.DeadlineAt
	if deadline.Validate() != nil {
		t.Fatal("root and stage minimum rejected")
	}
	raw, err := json.Marshal(deadline)
	if err != nil {
		t.Fatal(err)
	}
	var encoded map[string]any
	if json.Unmarshal(raw, &encoded) != nil {
		t.Fatal("fixture encoding failed")
	}
	if _, err = DecodeRuntimeExecutionDeadline(encoded); err != nil {
		t.Fatal("canonical JSON deadline rejected")
	}
	encoded["callerDeadline"] = true
	if _, err = DecodeRuntimeExecutionDeadline(encoded); err == nil {
		t.Fatal("unknown deadline authority accepted")
	}
}
