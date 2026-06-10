package runtime

import "context"

type SmokeRunInput struct {
	RunID string
	Role  string
}

type DeveloperRunInput struct {
	RunID               string
	Profile             string
	CodexAuthSecretName string
	GitHubSecretName    string
	Provider            string
	Owner               string
	Name                string
	BaseBranch          string
	HeadBranch          string
	Title               string
	Task                string
	Prompt              string
}

type ReviewRunInput struct {
	RunID               string
	Profile             string
	CodexAuthSecretName string
	GitHubSecretName    string
	Provider            string
	Owner               string
	Name                string
	PRNumber            int
	Prompt              string
}

type CodexAuthSessionInput struct {
	AccountName string
	SecretName  string
}

type CodexAuthSession struct {
	AccountName string
	SecretName  string
	Namespace   string
	JobName     string
	Created     bool
}

type CodexAuthStatus struct {
	AccountName  string
	SecretName   string
	Namespace    string
	JobName      string
	PodName      string
	Exists       bool
	JobActive    int32
	JobSucceeded int32
	JobFailed    int32
	PodPhase     string
	DeviceURL    string
	DeviceCode   string
	AuthReady    bool
	LogTail      string
}

type CodexAuthCompleteInput struct {
	AccountName string
	SecretName  string
}

type CodexAuthCompleteResult struct {
	AccountName string
	SecretName  string
	Namespace   string
	Saved       bool
}

type CodexAuthCleanupResult struct {
	AccountName string
	Namespace   string
	JobDeleted  bool
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
	Artifacts    map[string]string
}

type CleanupResult struct {
	RunID      string
	Namespace  string
	JobDeleted bool
	PVCDeleted bool
}

type Runner interface {
	StartSmokeRun(ctx context.Context, input SmokeRunInput) (StartedRun, error)
	StartCodexAuthSession(ctx context.Context, input CodexAuthSessionInput) (CodexAuthSession, error)
	GetCodexAuthStatus(ctx context.Context, accountName string, secretName string) (CodexAuthStatus, error)
	CompleteCodexAuthSession(ctx context.Context, input CodexAuthCompleteInput) (CodexAuthCompleteResult, error)
	CleanupCodexAuthSession(ctx context.Context, accountName string) (CodexAuthCleanupResult, error)
	StartDeveloperRun(ctx context.Context, input DeveloperRunInput) (StartedRun, error)
	StartReviewRun(ctx context.Context, input ReviewRunInput) (StartedRun, error)
	GetRunStatus(ctx context.Context, runID string) (RunStatus, error)
	CleanupRun(ctx context.Context, runID string) (CleanupResult, error)
}
