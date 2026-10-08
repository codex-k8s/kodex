package runtimecontract

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"
)

const WorkflowExecutionDeadlinePolicy = "workflow-wall-clock-v1"

func DecodeRuntimeExecutionDeadline(value any) (*RuntimeExecutionDeadline, error) {
	if value == nil {
		return nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 64<<10 {
		return nil, errors.New("runtime execution deadline encoding is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var deadline RuntimeExecutionDeadline
	if decoder.Decode(&deadline) != nil || deadline.Validate() != nil {
		return nil, errors.New("runtime execution deadline is invalid")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, errors.New("runtime execution deadline encoding is invalid")
	}
	return &deadline, nil
}

// Часы принадлежат owner Run: очередь до первого claim не входит в срок,
// но Human Gate, continuation и reclaim не приостанавливают и не сбрасывают его.
type RuntimeExecutionClock struct {
	RunRef                string    `json:"run_ref"`
	WorkflowVersionRef    string    `json:"workflow_version_ref"`
	WorkflowVersionDigest string    `json:"workflow_version_digest"`
	StepKey               string    `json:"step_key,omitempty"`
	TimeoutSeconds        int32     `json:"timeout_seconds"`
	StartedAt             time.Time `json:"started_at"`
	DeadlineAt            time.Time `json:"deadline_at"`
}

type RuntimeExecutionDeadline struct {
	Policy              string                  `json:"policy"`
	EffectiveDeadlineAt time.Time               `json:"effective_deadline_at"`
	Clocks              []RuntimeExecutionClock `json:"clocks"`
}

func (deadline *RuntimeExecutionDeadline) Validate() error {
	if deadline == nil {
		return nil
	}
	if deadline.Policy != WorkflowExecutionDeadlinePolicy || len(deadline.Clocks) == 0 || len(deadline.Clocks) > 128 {
		return errors.New("runtime execution deadline is invalid")
	}
	var effective time.Time
	seen := make(map[string]bool, len(deadline.Clocks))
	for _, clock := range deadline.Clocks {
		limit := int32(604800)
		if clock.StepKey != "" {
			limit = 86400
		}
		if !opaqueReferencePattern.MatchString(clock.RunRef) || !opaqueReferencePattern.MatchString(clock.WorkflowVersionRef) ||
			!sha256Pattern.MatchString(clock.WorkflowVersionDigest) || len(clock.StepKey) > 96 || clock.TimeoutSeconds < 1 || clock.TimeoutSeconds > limit ||
			clock.StartedAt.IsZero() || clock.StartedAt.Location() != time.UTC || clock.DeadlineAt.Location() != time.UTC ||
			!clock.DeadlineAt.Equal(clock.StartedAt.Add(time.Duration(clock.TimeoutSeconds)*time.Second)) || seen[clock.RunRef] {
			return errors.New("runtime execution clock is invalid")
		}
		seen[clock.RunRef] = true
		if effective.IsZero() || clock.DeadlineAt.Before(effective) {
			effective = clock.DeadlineAt
		}
	}
	if !deadline.EffectiveDeadlineAt.Equal(effective) || deadline.EffectiveDeadlineAt.Location() != time.UTC {
		return errors.New("runtime effective execution deadline is invalid")
	}
	return nil
}

// Ordinary Agent сохраняет прежний controller fallback. Workflow использует
// точный owner deadline даже когда его срок больше fallback.
func (input RunnerInput) BoundExecutionDeadline(ctx context.Context, fallback time.Duration) (context.Context, context.CancelFunc) {
	if input.ExecutionDeadline != nil {
		return context.WithDeadline(ctx, input.ExecutionDeadline.EffectiveDeadlineAt)
	}
	if fallback > 0 {
		return context.WithTimeout(ctx, fallback)
	}
	return context.WithCancel(ctx)
}
