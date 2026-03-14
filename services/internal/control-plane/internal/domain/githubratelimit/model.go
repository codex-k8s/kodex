package githubratelimit

import (
	"context"
	"encoding/json"
	"time"

	floweventdomain "github.com/codex-k8s/codex-k8s/libs/go/domain/flowevent"
	agentrunrepo "github.com/codex-k8s/codex-k8s/services/internal/control-plane/internal/domain/repository/agentrun"
	floweventrepo "github.com/codex-k8s/codex-k8s/services/internal/control-plane/internal/domain/repository/flowevent"
	waitrepo "github.com/codex-k8s/codex-k8s/services/internal/control-plane/internal/domain/repository/githubratelimitwait"
	enumtypes "github.com/codex-k8s/codex-k8s/services/internal/control-plane/internal/domain/types/enum"
	valuetypes "github.com/codex-k8s/codex-k8s/services/internal/control-plane/internal/domain/types/value"
)

const (
	primaryLimitGuardDelay              = 5 * time.Second
	secondaryRetryAfterGuardDelay       = 15 * time.Second
	primaryConservativeRetryDelay       = time.Minute
	secondaryBackoffBaseDelay           = time.Minute
	secondaryBackoffMaxDelay            = 15 * time.Minute
	signalExcerptMaxBytes               = 4 * 1024
	rateLimitResumePayloadMaxBytes      = 12 * 1024
	waitStateWaitingBackpressure        = "waiting_backpressure"
	runStatusPending                    = "pending"
	runStatusRunning                    = "running"
	runStatusWaitingBackpressure        = "waiting_backpressure"
	runStatusSucceeded                  = "succeeded"
	runStatusFailed                     = "failed"
	runStatusCanceled                   = "canceled"
	githubRateLimitWaitEnteredEventKey  = "github_rate_limit.wait.entered"
	githubRateLimitManualActionEventKey = "github_rate_limit.manual_action_required"
)

// Config controls rollout gates for GitHub rate-limit ownership inside control-plane.
type Config struct {
	RolloutState valuetypes.GitHubRateLimitRolloutState
}

// Dependencies contains collaborators required by the domain service.
type Dependencies struct {
	Runs       runReader
	Waits      waitrepo.Repository
	FlowEvents floweventrepo.Repository
}

// ReportSignalParams carries one canonical provider signal into control-plane.
type ReportSignalParams struct {
	RunID  string
	Signal Signal
}

// ReportSignalResult returns persisted wait state, projection, and comment context for one signal.
type ReportSignalResult struct {
	HardFailure          bool
	DuplicateSignal      bool
	Wait                 Wait
	Classification       Classification
	Projection           WaitProjection
	CommentRenderContext CommentRenderContext
	ProjectionRefresh    waitrepo.RefreshProjectionResult
}

// BuildResumePayloadParams describes deterministic agent-session resume JSON emitted after recovery.
type BuildResumePayloadParams struct {
	Wait           Wait
	ResolutionKind enumtypes.GitHubRateLimitResolutionKind
	RecoveredAt    time.Time
	AttemptNo      int
}

// Service implements canonical GitHub rate-limit domain ownership under control-plane.
type Service struct {
	cfg          Config
	runs         runReader
	waits        waitrepo.Repository
	flowEvents   floweventrepo.Repository
	capabilities valuetypes.GitHubRateLimitRolloutCapabilities
	now          func() time.Time
}

type runReader interface {
	GetByID(ctx context.Context, runID string) (agentrunrepo.Run, bool, error)
}

type waitSignalEvidencePayload struct {
	ContourKind            enumtypes.GitHubRateLimitContourKind    `json:"contour_kind"`
	OperationClass         enumtypes.GitHubRateLimitOperationClass `json:"operation_class"`
	SessionSnapshotVersion *int64                                  `json:"session_snapshot_version,omitempty"`
}

type waitClassificationEvidencePayload struct {
	LimitKind           enumtypes.GitHubRateLimitLimitKind           `json:"limit_kind"`
	Confidence          enumtypes.GitHubRateLimitConfidence          `json:"confidence"`
	RecoveryHintKind    enumtypes.GitHubRateLimitRecoveryHintKind    `json:"recovery_hint_kind"`
	RecoveryHintSource  enumtypes.GitHubRateLimitRecoveryHintSource  `json:"recovery_hint_source"`
	NextStepKind        enumtypes.GitHubRateLimitNextStepKind        `json:"next_step_kind"`
	ResumeActionKind    enumtypes.GitHubRateLimitResumeActionKind    `json:"resume_action_kind"`
	ManualActionKind    enumtypes.GitHubRateLimitManualActionKind    `json:"manual_action_kind,omitempty"`
	ProjectionSyncState enumtypes.GitHubRateLimitProjectionSyncState `json:"projection_sync_state"`
}

type runWaitFlowEventPayload struct {
	RunID              string                                      `json:"run_id"`
	WaitID             string                                      `json:"wait_id"`
	ContourKind        enumtypes.GitHubRateLimitContourKind        `json:"contour_kind"`
	LimitKind          enumtypes.GitHubRateLimitLimitKind          `json:"limit_kind"`
	State              enumtypes.GitHubRateLimitWaitState          `json:"state"`
	NextStepKind       enumtypes.GitHubRateLimitNextStepKind       `json:"next_step_kind"`
	ResumeNotBefore    *time.Time                                  `json:"resume_not_before,omitempty"`
	OpenWaitCount      int                                         `json:"open_wait_count"`
	DominantWaitID     string                                      `json:"dominant_wait_id,omitempty"`
	EventKey           string                                      `json:"event_key"`
	CommentMirrorState enumtypes.GitHubRateLimitCommentMirrorState `json:"comment_mirror_state"`
}

type marshalErrorPayload struct {
	Error string `json:"error"`
}

type messageTemplateData struct {
	ContourKind               string
	LimitKind                 string
	OperationClass            string
	WaitState                 string
	Confidence                string
	RecoveryHintKind          string
	NextStepKind              string
	ResumeNotBeforeRFC3339    string
	SuggestedNotBeforeRFC3339 string
	AttemptsUsed              int
	MaxAttempts               int
	AttemptNo                 int
}

func marshalJSONPayload(payload any) json.RawMessage {
	raw, err := json.Marshal(payload)
	if err == nil {
		return raw
	}
	fallback, fallbackErr := json.Marshal(marshalErrorPayload{Error: "payload_marshal_failed"})
	if fallbackErr != nil {
		return json.RawMessage(`{"error":"payload_marshal_failed"}`)
	}
	return fallback
}

func waitPausedEventType() floweventdomain.EventType {
	return floweventdomain.EventTypeRunWaitPaused
}
