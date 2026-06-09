package runtime

import "context"

type SmokeRunInput struct {
	RunID string
	Role  string
}

type StartedRun struct {
	RunID     string
	Namespace string
	JobName   string
	PVCName   string
	Created   bool
}

type RunStatus struct {
	RunID        string
	Namespace    string
	JobName      string
	PVCName      string
	PodName      string
	Exists       bool
	JobActive    int32
	JobSucceeded int32
	JobFailed    int32
	PodPhase     string
	LogTail      string
}

type CleanupResult struct {
	RunID      string
	Namespace  string
	JobDeleted bool
	PVCDeleted bool
}

type Runner interface {
	StartSmokeRun(ctx context.Context, input SmokeRunInput) (StartedRun, error)
	GetRunStatus(ctx context.Context, runID string) (RunStatus, error)
	CleanupRun(ctx context.Context, runID string) (CleanupResult, error)
}
