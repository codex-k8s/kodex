package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	missionControlSyntheticDeliveryPrefix = "mission-control"
	missionControlStatusAccepted          = "accepted"
	missionControlCommandKindStageNext    = "stage.next_step.execute"
	missionControlFailureProviderError    = "provider_error"
	missionControlFailureUnknown          = "unknown"
)

// MissionControlWarmupProject describes one project that needs Mission Control warmup.
type MissionControlWarmupProject struct {
	ProjectID          string
	ProjectName        string
	RepositoryFullName string
}

// MissionControlWarmupResult returns the current warmup evidence after one run.
type MissionControlWarmupResult struct {
	ProjectID                    string
	EntityCount                  int64
	RelationCount                int64
	TimelineEntryCount           int64
	CommandCount                 int64
	MaxProjectionVersion         int64
	RunEntityCount               int64
	LegacyAgentCount             int64
	ContinuityGapCount           int64
	OpenGapCount                 int64
	BlockingGapCount             int64
	MissingPullRequestGapCount   int64
	MissingFollowUpIssueGapCount int64
	WatermarkCount               int64
	ReadyForReconcile            bool
	ReconcileGatingReason        string
	ReadyForTransport            bool
	TransportGatingReason        string
	ProviderFreshnessStatus      string
	ProviderCoverageStatus       string
	GraphProjectionStatus        string
	LaunchPolicyStatus           string
	BackfilledEntities           int
	BackfilledRelations          int
	BackfilledTimelines          int
}

// MissionControlStageNextStepPayload contains the executable stage transition details.
type MissionControlStageNextStepPayload struct {
	ThreadKind  string
	ThreadNo    int
	TargetLabel string
}

// MissionControlPendingCommand describes one Mission Control command ready for execution.
type MissionControlPendingCommand struct {
	ProjectID            string
	CommandID            string
	CommandKind          string
	EffectiveCommandKind string
	Status               string
	CorrelationID        string
	BusinessIntentKey    string
	RepositoryFullName   string
	RetryTargetCommandID string
	StageNextStep        *MissionControlStageNextStepPayload
	RequestedAt          time.Time
	UpdatedAt            time.Time
}

// MissionControlCommandState is the typed response for one persisted command transition.
type MissionControlCommandState struct {
	ProjectID           string
	CommandID           string
	CommandKind         string
	Status              string
	FailureReason       string
	CorrelationID       string
	ProviderDeliveryIDs []string
	StatusMessage       string
	UpdatedAt           time.Time
	ReconciledAt        *time.Time
}

// MissionControlQueueCommandParams requests transition to queued state.
type MissionControlQueueCommandParams struct {
	ProjectID     string
	CommandID     string
	StatusMessage string
	UpdatedAt     time.Time
}

// MissionControlPendingSyncParams requests transition to pending_sync state.
type MissionControlPendingSyncParams struct {
	ProjectID           string
	CommandID           string
	ProviderDeliveryIDs []string
	StatusMessage       string
	UpdatedAt           time.Time
}

// MissionControlReconciledParams requests transition to reconciled state.
type MissionControlReconciledParams struct {
	ProjectID           string
	CommandID           string
	ProviderDeliveryIDs []string
	StatusMessage       string
	UpdatedAt           time.Time
	ReconciledAt        time.Time
}

// MissionControlFailedParams requests transition to failed state.
type MissionControlFailedParams struct {
	ProjectID           string
	CommandID           string
	FailureReason       string
	ProviderDeliveryIDs []string
	StatusMessage       string
	UpdatedAt           time.Time
}

// NextStepExecuteParams describes one provider-safe stage.next_step execution request.
type NextStepExecuteParams struct {
	RepositoryFullName string
	ThreadKind         string
	ThreadNo           int
	TargetLabel        string
}

// MissionControlClient exposes worker-facing Mission Control RPCs.
type MissionControlClient interface {
	ListMissionControlWarmupProjects(ctx context.Context, limit int) ([]MissionControlWarmupProject, error)
	RunMissionControlWarmup(ctx context.Context, projectID string, requestedBy string, correlationID string, forceRebuild bool) (MissionControlWarmupResult, error)
	ClaimMissionControlPendingCommands(ctx context.Context, workerID string, leaseTTL time.Duration, limit int) ([]MissionControlPendingCommand, error)
	QueueMissionControlCommand(ctx context.Context, params MissionControlQueueCommandParams) (MissionControlCommandState, error)
	MarkMissionControlCommandPendingSync(ctx context.Context, params MissionControlPendingSyncParams) (MissionControlCommandState, error)
	MarkMissionControlCommandReconciled(ctx context.Context, params MissionControlReconciledParams) (MissionControlCommandState, error)
	MarkMissionControlCommandFailed(ctx context.Context, params MissionControlFailedParams) (MissionControlCommandState, error)
	ExecuteNextStepAction(ctx context.Context, params NextStepExecuteParams) error
}

func (s *Service) reconcileMissionControl(ctx context.Context) error {
	if s.missionCtl == nil {
		return nil
	}
	var errs []error
	if err := s.reconcileMissionControlWarmups(ctx); err != nil {
		errs = append(errs, fmt.Errorf("reconcile mission control warmups: %w", err))
	}
	if err := s.reconcileMissionControlCommands(ctx); err != nil {
		errs = append(errs, fmt.Errorf("reconcile mission control commands: %w", err))
	}
	return errors.Join(errs...)
}

func (s *Service) reconcileMissionControlWarmups(ctx context.Context) error {
	projects, err := s.missionCtl.ListMissionControlWarmupProjects(ctx, s.cfg.MissionControlWarmupProjectLimit)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	var errs []error
	for _, project := range projects {
		projectID := strings.TrimSpace(project.ProjectID)
		lastWarmupAt, ok := s.lastMissionControlWarmup[projectID]
		if ok && now.Sub(lastWarmupAt) < s.cfg.MissionControlWarmupInterval {
			continue
		}
		correlationID := fmt.Sprintf("%s:warmup:%s:%d", missionControlSyntheticDeliveryPrefix, projectID, now.Unix())
		result, warmupErr := s.missionCtl.RunMissionControlWarmup(ctx, projectID, s.cfg.WorkerID, correlationID, false)
		if warmupErr != nil {
			errs = append(errs, fmt.Errorf(
				"project %s (%s): %w",
				projectID,
				strings.TrimSpace(project.RepositoryFullName),
				warmupErr,
			))
			continue
		}
		s.lastMissionControlWarmup[projectID] = now
		s.logMissionControlWarmupResult(project, correlationID, result)
	}
	return errors.Join(errs...)
}

func (s *Service) reconcileMissionControlCommands(ctx context.Context) error {
	commands, err := s.missionCtl.ClaimMissionControlPendingCommands(
		ctx,
		s.cfg.WorkerID,
		s.cfg.MissionControlClaimTTL,
		s.cfg.MissionControlPendingCommandLimit,
	)
	if err != nil {
		return err
	}
	var errs []error
	for _, command := range commands {
		if commandErr := s.processMissionControlCommand(ctx, command); commandErr != nil {
			errs = append(errs, fmt.Errorf(
				"project %s command %s (%s): %w",
				strings.TrimSpace(command.ProjectID),
				strings.TrimSpace(command.CommandID),
				strings.TrimSpace(command.EffectiveCommandKind),
				commandErr,
			))
		}
	}
	return errors.Join(errs...)
}

func (s *Service) processMissionControlCommand(ctx context.Context, command MissionControlPendingCommand) error {
	status := strings.TrimSpace(strings.ToLower(command.Status))
	if status == missionControlStatusAccepted {
		if _, err := s.missionCtl.QueueMissionControlCommand(ctx, MissionControlQueueCommandParams{
			ProjectID:     command.ProjectID,
			CommandID:     command.CommandID,
			StatusMessage: "queued by worker",
			UpdatedAt:     s.now().UTC(),
		}); err != nil {
			return err
		}
	}

	switch strings.TrimSpace(strings.ToLower(command.EffectiveCommandKind)) {
	case missionControlCommandKindStageNext:
		return s.executeMissionControlStageNextStep(ctx, command)
	default:
		_, err := s.missionCtl.MarkMissionControlCommandFailed(ctx, MissionControlFailedParams{
			ProjectID:     command.ProjectID,
			CommandID:     command.CommandID,
			FailureReason: missionControlFailureUnknown,
			StatusMessage: "unsupported mission control command kind",
			UpdatedAt:     s.now().UTC(),
		})
		return err
	}
}

func (s *Service) executeMissionControlStageNextStep(ctx context.Context, command MissionControlPendingCommand) error {
	if command.StageNextStep == nil || strings.TrimSpace(command.RepositoryFullName) == "" {
		_, err := s.missionCtl.MarkMissionControlCommandFailed(ctx, MissionControlFailedParams{
			ProjectID:     command.ProjectID,
			CommandID:     command.CommandID,
			FailureReason: missionControlFailureUnknown,
			StatusMessage: "stage.next_step payload is incomplete",
			UpdatedAt:     s.now().UTC(),
		})
		return err
	}

	var executeErr error
	for attempt := 1; attempt <= s.cfg.MissionControlRetryMaxAttempts; attempt++ {
		executeErr = s.missionCtl.ExecuteNextStepAction(ctx, NextStepExecuteParams{
			RepositoryFullName: command.RepositoryFullName,
			ThreadKind:         command.StageNextStep.ThreadKind,
			ThreadNo:           command.StageNextStep.ThreadNo,
			TargetLabel:        command.StageNextStep.TargetLabel,
		})
		if executeErr == nil {
			deliveryID := missionControlSyntheticDeliveryID(command.CommandID)
			if _, err := s.missionCtl.MarkMissionControlCommandPendingSync(ctx, MissionControlPendingSyncParams{
				ProjectID:           command.ProjectID,
				CommandID:           command.CommandID,
				ProviderDeliveryIDs: []string{deliveryID},
				StatusMessage:       "provider mutation accepted",
				UpdatedAt:           s.now().UTC(),
			}); err != nil {
				return err
			}
			_, err := s.missionCtl.MarkMissionControlCommandReconciled(ctx, MissionControlReconciledParams{
				ProjectID:           command.ProjectID,
				CommandID:           command.CommandID,
				ProviderDeliveryIDs: []string{deliveryID},
				StatusMessage:       "provider mutation reconciled",
				UpdatedAt:           s.now().UTC(),
				ReconciledAt:        s.now().UTC(),
			})
			return err
		}
		if attempt >= s.cfg.MissionControlRetryMaxAttempts {
			break
		}
		if err := sleepWithContext(ctx, missionControlRetryDelay(s.cfg.MissionControlRetryBaseInterval, attempt)); err != nil {
			return err
		}
	}

	_, err := s.missionCtl.MarkMissionControlCommandFailed(ctx, MissionControlFailedParams{
		ProjectID:     command.ProjectID,
		CommandID:     command.CommandID,
		FailureReason: missionControlFailureProviderError,
		StatusMessage: executeErr.Error(),
		UpdatedAt:     s.now().UTC(),
	})
	if err != nil {
		return err
	}
	return nil
}

func missionControlSyntheticDeliveryID(commandID string) string {
	return missionControlSyntheticDeliveryPrefix + ":" + strings.TrimSpace(commandID)
}

func missionControlRetryDelay(base time.Duration, attempt int) time.Duration {
	if attempt <= 1 {
		return base
	}
	delay := base
	for i := 1; i < attempt; i++ {
		delay *= 2
	}
	return delay
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}

func (s *Service) logMissionControlWarmupResult(project MissionControlWarmupProject, correlationID string, result MissionControlWarmupResult) {
	if !result.ReadyForTransport {
		s.logger.Info(
			"mission control warmup completed with open rollout gates",
			"project_id", strings.TrimSpace(result.ProjectID),
			"project_name", strings.TrimSpace(project.ProjectName),
			"repository_full_name", strings.TrimSpace(project.RepositoryFullName),
			"correlation_id", strings.TrimSpace(correlationID),
			"entity_count", result.EntityCount,
			"run_entity_count", result.RunEntityCount,
			"continuity_gap_count", result.ContinuityGapCount,
			"open_gap_count", result.OpenGapCount,
			"blocking_gap_count", result.BlockingGapCount,
			"watermark_count", result.WatermarkCount,
			"ready_for_reconcile", result.ReadyForReconcile,
			"reconcile_gating_reason", strings.TrimSpace(result.ReconcileGatingReason),
			"ready_for_transport", result.ReadyForTransport,
			"transport_gating_reason", strings.TrimSpace(result.TransportGatingReason),
			"provider_freshness_status", strings.TrimSpace(result.ProviderFreshnessStatus),
			"provider_coverage_status", strings.TrimSpace(result.ProviderCoverageStatus),
			"graph_projection_status", strings.TrimSpace(result.GraphProjectionStatus),
			"launch_policy_status", strings.TrimSpace(result.LaunchPolicyStatus),
		)
		return
	}

	s.logger.Info(
		"mission control warmup completed",
		"project_id", strings.TrimSpace(result.ProjectID),
		"project_name", strings.TrimSpace(project.ProjectName),
		"repository_full_name", strings.TrimSpace(project.RepositoryFullName),
		"correlation_id", strings.TrimSpace(correlationID),
		"entity_count", result.EntityCount,
		"run_entity_count", result.RunEntityCount,
		"continuity_gap_count", result.ContinuityGapCount,
		"open_gap_count", result.OpenGapCount,
		"blocking_gap_count", result.BlockingGapCount,
		"watermark_count", result.WatermarkCount,
		"ready_for_reconcile", result.ReadyForReconcile,
		"ready_for_transport", result.ReadyForTransport,
	)
}
