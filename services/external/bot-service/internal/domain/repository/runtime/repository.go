package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type SecretIntegrityInput struct {
	SecretName string
	SecretKey  string
}

type SecretIntegrity struct {
	SecretName      string
	SecretKey       string
	ContentSHA256   string
	UID             string
	ResourceVersion string
}

var ErrAgentSessionCapacity = errors.New("agent session runtime capacity unavailable")

type AgentSessionCapacityError struct {
	Reason string
	Cause  error
}

func (err *AgentSessionCapacityError) Error() string {
	if err == nil {
		return ErrAgentSessionCapacity.Error()
	}
	if err.Cause == nil {
		return fmt.Sprintf("%s: %s", ErrAgentSessionCapacity, err.Reason)
	}
	return fmt.Sprintf("%s: %s: %v", ErrAgentSessionCapacity, err.Reason, err.Cause)
}

func (err *AgentSessionCapacityError) Is(target error) bool {
	return target == ErrAgentSessionCapacity
}

func (err *AgentSessionCapacityError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

func NewAgentSessionCapacityError(reason string, cause error) error {
	return &AgentSessionCapacityError{Reason: reason, Cause: cause}
}

func IsAgentSessionCapacityError(err error) bool {
	return errors.Is(err, ErrAgentSessionCapacity)
}

type SmokeRunInput struct {
	RunID string
	Role  string
}

type DeveloperRunInput struct {
	RunID               string
	Profile             string
	KubernetesAccess    string
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
	SandboxMode         string
	ConfigOverlay       string
	RuntimeEnv          []RuntimeEnvVar
}

type ReviewRunInput struct {
	RunID               string
	Profile             string
	KubernetesAccess    string
	CodexAuthSecretName string
	GitHubSecretName    string
	Provider            string
	Owner               string
	Name                string
	PRNumber            int
	Prompt              string
	SandboxMode         string
	ConfigOverlay       string
	RuntimeEnv          []RuntimeEnvVar
}

type ChatRunInput struct {
	RunID               string
	Profile             string
	KubernetesAccess    string
	CodexAuthSecretName string
	GitHubSecretName    string
	Prompt              string
	SandboxMode         string
	ConfigOverlay       string
	RuntimeEnv          []RuntimeEnvVar
}

type AgentSessionPodInput struct {
	SessionKey              string
	Role                    string
	KubernetesAccess        string
	BotServiceURL           string
	InternalToken           string
	CodexAuthSecretName     string
	GitHubSecretName        string
	RepositoryProvider      string
	RepositoryOwner         string
	RepositoryName          string
	RepositoryDefaultBranch string
	SandboxMode             string
	ConfigOverlay           string
	RuntimeEnv              []RuntimeEnvVar
	TokenSecretIntegrity    *SecretIntegrity
	PodTokenSecretName      string
}

type AgentSessionRuntimeHealth struct {
	SessionKey string
	Namespace  string
	PodName    string
	Exists     bool
	Phase      string
	Terminal   bool
	Reason     string
}

type RuntimeEnvVar struct {
	Name        string
	SecretName  string
	SecretKey   string
	Description string
	Sensitive   bool
}

type StartedAgentSession struct {
	SessionKey string
	Namespace  string
	PodName    string
	PVCName    string
	SecretName string
	Created    bool
}

type AgentSessionCleanupResult struct {
	SessionKey string
	Namespace  string
	PodName    string
	PodDeleted bool
}

type MattermostBotTokenSecretInput struct {
	SecretName string
	Token      string
}

type MattermostBotTokenSecret struct {
	SecretName string
	Namespace  string
	Created    bool
	Token      string
	Integrity  SecretIntegrity
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

type CodexAuthSecretCheckInput struct {
	AccountName string
	SecretName  string
}

type CodexAuthSecretCheckResult struct {
	AccountName string
	SecretName  string
	Namespace   string
	JobName     string
	PodName     string
	PodPhase    string
	Ready       bool
	LogTail     string
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

type CodexAuthAccountDeleteResult struct {
	AccountName   string
	SecretName    string
	Namespace     string
	JobDeleted    bool
	SecretDeleted bool
}

type GitHubTokenSecretInput struct {
	AccountName string
	SecretName  string
	Token       string
	Username    string
	Email       string
}

type GitHubTokenSecret struct {
	AccountName string
	SecretName  string
	Namespace   string
	Created     bool
}

type ProjectRuntimeVariableSecretInput struct {
	ProjectSlug string
	Variable    RuntimeEnvVar
	Value       string
}

type ProjectRuntimeVariableSecret struct {
	SecretName string
	Namespace  string
	Created    bool
}

type GitHubTokenSecretCredential struct {
	AccountName string
	SecretName  string
	Namespace   string
	Token       string
	Username    string
	Email       string
}

type GitHubTokenSecretDeleteResult struct {
	AccountName   string
	SecretName    string
	Namespace     string
	SecretDeleted bool
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

type RetentionCleanupInput struct {
	OlderThan time.Duration
	Now       time.Time
	DryRun    bool
}

type RetentionCleanupResult struct {
	Namespace             string
	DryRun                bool
	OlderThan             time.Duration
	RunsMatched           int
	SkippedActiveJobs     int
	JobsMatched           int
	JobsDeleted           int
	PVCsMatched           int
	PVCsDeleted           int
	ConfigMapsMatched     int
	ConfigMapsDeleted     int
	SessionPodsMatched    int
	SessionPodsDeleted    int
	SessionPVCsMatched    int
	SessionPVCsDeleted    int
	SessionSecretsMatched int
	SessionSecretsDeleted int
	MatchedRunIDs         []string
}

type Runner interface {
	InspectSecretIntegrity(ctx context.Context, input SecretIntegrityInput) (SecretIntegrity, error)
	StartSmokeRun(ctx context.Context, input SmokeRunInput) (StartedRun, error)
	StartCodexAuthSession(ctx context.Context, input CodexAuthSessionInput) (CodexAuthSession, error)
	GetCodexAuthStatus(ctx context.Context, accountName string, secretName string) (CodexAuthStatus, error)
	CheckCodexAuthSecret(ctx context.Context, input CodexAuthSecretCheckInput) (CodexAuthSecretCheckResult, error)
	CompleteCodexAuthSession(ctx context.Context, input CodexAuthCompleteInput) (CodexAuthCompleteResult, error)
	CleanupCodexAuthSession(ctx context.Context, accountName string) (CodexAuthCleanupResult, error)
	DeleteCodexAuthAccount(ctx context.Context, accountName string, secretName string) (CodexAuthAccountDeleteResult, error)
	UpsertGitHubTokenSecret(ctx context.Context, input GitHubTokenSecretInput) (GitHubTokenSecret, error)
	DeleteGitHubTokenSecret(ctx context.Context, accountName string, secretName string) (GitHubTokenSecretDeleteResult, error)
	UpsertProjectRuntimeVariableSecret(ctx context.Context, input ProjectRuntimeVariableSecretInput) (ProjectRuntimeVariableSecret, error)
	DeleteProjectRuntimeVariableSecret(ctx context.Context, secretName string) (ProjectRuntimeVariableSecret, error)
	StartDeveloperRun(ctx context.Context, input DeveloperRunInput) (StartedRun, error)
	StartReviewRun(ctx context.Context, input ReviewRunInput) (StartedRun, error)
	StartChatRun(ctx context.Context, input ChatRunInput) (StartedRun, error)
	StartAgentSession(ctx context.Context, input AgentSessionPodInput) (StartedAgentSession, error)
	GetAgentSessionRuntimeHealth(ctx context.Context, sessionKey string) (AgentSessionRuntimeHealth, error)
	CleanupAgentSession(ctx context.Context, sessionKey string) (AgentSessionCleanupResult, error)
	UpsertMattermostBotTokenSecret(ctx context.Context, input MattermostBotTokenSecretInput) (MattermostBotTokenSecret, error)
	GetMattermostBotTokenSecret(ctx context.Context, secretName string) (MattermostBotTokenSecret, error)
	GetRunStatus(ctx context.Context, runID string) (RunStatus, error)
	CleanupRun(ctx context.Context, runID string) (CleanupResult, error)
	CleanupExpiredRuns(ctx context.Context, input RetentionCleanupInput) (RetentionCleanupResult, error)
}

type GitHubTokenSecretReader interface {
	GetGitHubTokenSecret(ctx context.Context, accountName string, secretName string) (GitHubTokenSecretCredential, error)
}
