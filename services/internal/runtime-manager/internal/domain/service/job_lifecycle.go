package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	runtimerepo "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/repository/runtime"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/value"
)

const (
	maxShortLogTailBytes    = 16 * 1024
	maxRuntimeArtifactRefs  = 16
	defaultJobFailureAction = "review_runtime_job_failure"
)

// CreateJob creates a pending platform job without starting executor-specific work.
func (s *Service) CreateJob(ctx context.Context, input CreateJobInput) (entity.Job, error) {
	resolved, err := s.resolveJobCreateInput(ctx, input)
	if err != nil {
		return entity.Job{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, actionJobCreate, jobResource(uuid.Nil, resolved.ProjectID)); err != nil {
		return entity.Job{}, err
	}
	if replay, result, ok, err := s.createJobReplay(ctx, input.Meta); err != nil || ok {
		if err == nil {
			err = validateJobReplayScope(replay, resolved, result)
		}
		return replay, err
	}
	if input.SlotID != nil && (resolved.FleetScopeID == nil || resolved.ClusterID == nil) {
		return entity.Job{}, errs.ErrPreconditionFailed
	}
	if input.SlotID == nil && (resolved.FleetScopeID == nil || resolved.ClusterID == nil) {
		placement, err := s.resolvePlacement(ctx, resolved.PlacementRequest)
		if err != nil {
			return entity.Job{}, err
		}
		resolved.FleetScopeID = &placement.FleetScopeID
		resolved.ClusterID = &placement.ClusterID
	}
	now := commandTime(input.Meta, s.clock.Now())
	jobID := s.ids.New()
	job := entity.Job{
		Base: entity.Base{
			ID:        jobID,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
		},
		JobType:               resolved.JobType,
		Status:                enum.JobStatusPending,
		Priority:              resolved.Priority,
		JobInputJSON:          resolved.JobInputJSON,
		SlotID:                resolved.SlotID,
		AgentRunID:            resolved.AgentRunID,
		ProjectID:             resolved.ProjectID,
		RepositoryID:          resolved.RepositoryID,
		ReleaseLineID:         resolved.ReleaseLineID,
		PackageInstallationID: resolved.PackageInstallationID,
		FleetScopeID:          resolved.FleetScopeID,
		ClusterID:             resolved.ClusterID,
		RequestedBy:           requestedBy(input.Meta.Actor),
	}
	resultPayload, err := createJobCommandPayload(resolved.PlacementFingerprint)
	if err != nil {
		return entity.Job{}, err
	}
	result, err := commandResult(input.Meta, operationCreateJob, aggregateTypeJob, job.ID, resultPayload, now)
	if err != nil {
		return entity.Job{}, err
	}
	job.CommandID = result.Key
	event, err := s.jobEvent(eventJobCreated, job, now)
	if err != nil {
		return entity.Job{}, err
	}
	return job, s.repository.CreateJob(ctx, job, event, result)
}

// ClaimRunnableJob leases one pending or expired job for a worker.
func (s *Service) ClaimRunnableJob(ctx context.Context, input ClaimRunnableJobInput) (ClaimRunnableJobResult, error) {
	if err := validateClaimJobInput(input, s.clock.Now()); err != nil {
		return ClaimRunnableJobResult{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, actionJobClaim, jobResource(uuid.Nil, nil)); err != nil {
		return ClaimRunnableJobResult{}, err
	}
	if _, replayed, err := s.findCommandResult(ctx, input.Meta, operationClaimJob, aggregateTypeJob); err != nil || replayed {
		if err == nil {
			err = errs.ErrConflict
		}
		return ClaimRunnableJobResult{}, err
	}
	leaseToken := s.ids.New().String()
	now := commandTime(input.Meta, s.clock.Now())
	filter := query.JobClaimFilter{
		JobTypes:       append([]enum.JobType(nil), input.JobTypes...),
		FleetScopeID:   input.FleetScopeID,
		LeaseOwner:     strings.TrimSpace(input.LeaseOwner),
		LeaseTokenHash: leaseTokenHash(leaseToken),
		LeaseUntil:     input.LeaseUntil,
		Now:            now,
	}
	recordFactory := func(job entity.Job) (entity.OutboxEvent, entity.CommandResult, error) {
		event, err := s.jobEvent(eventJobStarted, job, now)
		if err != nil {
			return entity.OutboxEvent{}, entity.CommandResult{}, err
		}
		result, err := commandResult(input.Meta, operationClaimJob, aggregateTypeJob, job.ID, nil, now)
		if err != nil {
			return entity.OutboxEvent{}, entity.CommandResult{}, err
		}
		return event, result, nil
	}
	job, err := s.repository.ClaimRunnableJob(ctx, filter, recordFactory)
	if err != nil {
		return ClaimRunnableJobResult{}, err
	}
	return ClaimRunnableJobResult{Job: job, LeaseToken: leaseToken}, nil
}

// ReportJobStepProgress updates one job step and stores bounded runtime diagnostics.
func (s *Service) ReportJobStepProgress(ctx context.Context, input ReportJobStepProgressInput) (entity.Job, error) {
	if err := validateReportJobStepInput(input); err != nil {
		return entity.Job{}, err
	}
	job, err := s.repository.GetJob(ctx, input.JobID)
	if err != nil {
		return entity.Job{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, actionJobStepReport, jobResource(job.ID, job.ProjectID)); err != nil {
		return entity.Job{}, err
	}
	if replay, ok, err := s.jobReplay(ctx, input.Meta, operationReportJobStep, &input.JobID); err != nil || ok {
		return replay, err
	}
	expected, err := expectedVersion(input.Meta)
	if err != nil {
		return entity.Job{}, err
	}
	if err := validateActiveJobLease(job, input.LeaseToken, s.clock.Now()); err != nil {
		return entity.Job{}, err
	}
	if job.Version != expected || terminalJobStatus(job.Status) {
		return entity.Job{}, errs.ErrConflict
	}
	now := commandTime(input.Meta, s.clock.Now())
	previousStatus := string(job.Status)
	step := upsertJobStep(job, input, now, s.ids.New)
	updateJobAfterStep(&job, input, now)
	refs, err := newRuntimeArtifactRefs(s.ids, input.ArtifactRefs, &job.ID, job.SlotID, now)
	if err != nil {
		return entity.Job{}, err
	}
	job.Steps = replaceJobStep(job.Steps, step)
	event, err := s.jobEvent(eventJobStepUpdated, job, now, payloadPreviousStatus(previousStatus), payloadJobStep(step))
	if err != nil {
		return entity.Job{}, err
	}
	result, err := commandResult(input.Meta, operationReportJobStep, aggregateTypeJob, job.ID, nil, now)
	if err != nil {
		return entity.Job{}, err
	}
	return job, s.repository.UpdateJob(ctx, job, expected, []entity.JobStep{step}, refs, &event, result)
}

// CompleteJob marks a leased job as successfully finished.
func (s *Service) CompleteJob(ctx context.Context, input CompleteJobInput) (entity.Job, error) {
	if err := validateTerminalJobInput(input.JobID, input.LeaseToken, input.Meta, operationCompleteJob); err != nil {
		return entity.Job{}, err
	}
	return s.finishJob(ctx, finishJobInput{
		jobID:        input.JobID,
		leaseToken:   input.LeaseToken,
		status:       enum.JobStatusSucceeded,
		eventType:    eventJobCompleted,
		shortLogTail: input.ShortLogTail,
		fullLogRef:   input.FullLogRef,
		meta:         input.Meta,
		operation:    operationCompleteJob,
		action:       actionJobComplete,
	})
}

// FailJob marks a leased job as failed with classified diagnostics.
func (s *Service) FailJob(ctx context.Context, input FailJobInput) (entity.Job, error) {
	if err := validateTerminalJobInput(input.JobID, input.LeaseToken, input.Meta, operationFailJob); err != nil {
		return entity.Job{}, err
	}
	if strings.TrimSpace(input.ErrorCode) == "" {
		return entity.Job{}, errs.ErrInvalidArgument
	}
	nextAction := strings.TrimSpace(input.NextAction)
	if nextAction == "" {
		nextAction = defaultJobFailureAction
	}
	return s.finishJob(ctx, finishJobInput{
		jobID:        input.JobID,
		leaseToken:   input.LeaseToken,
		status:       enum.JobStatusFailed,
		eventType:    eventJobFailed,
		errorCode:    input.ErrorCode,
		errorMessage: input.ErrorMessage,
		shortLogTail: input.ShortLogTail,
		fullLogRef:   input.FullLogRef,
		nextAction:   nextAction,
		meta:         input.Meta,
		operation:    operationFailJob,
		action:       actionJobFail,
	})
}

// CancelJob cancels a pending, claimed or running job by policy.
func (s *Service) CancelJob(ctx context.Context, input CancelJobInput) (entity.Job, error) {
	if input.JobID == uuid.Nil {
		return entity.Job{}, errs.ErrInvalidArgument
	}
	if _, err := commandIdentity(input.Meta, operationCancelJob); err != nil {
		return entity.Job{}, err
	}
	job, err := s.repository.GetJob(ctx, input.JobID)
	if err != nil {
		return entity.Job{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, actionJobCancel, jobResource(job.ID, job.ProjectID)); err != nil {
		return entity.Job{}, err
	}
	if replay, ok, err := s.jobReplay(ctx, input.Meta, operationCancelJob, &input.JobID); err != nil || ok {
		return replay, err
	}
	expected, err := expectedVersion(input.Meta)
	if err != nil {
		return entity.Job{}, err
	}
	if job.Version != expected || terminalJobStatus(job.Status) {
		return entity.Job{}, errs.ErrConflict
	}
	now := commandTime(input.Meta, s.clock.Now())
	previousStatus := string(job.Status)
	job.Status = enum.JobStatusCancelled
	job.LeaseOwner = ""
	job.LeaseTokenHash = ""
	job.LeaseUntil = nil
	job.FinishedAt = timePtr(now)
	job.NextAction = ""
	job.UpdatedAt = now
	job.Version = expected + 1
	event, err := s.jobEvent(eventJobCancelled, job, now, payloadPreviousStatus(previousStatus))
	if err != nil {
		return entity.Job{}, err
	}
	result, err := commandResult(input.Meta, operationCancelJob, aggregateTypeJob, job.ID, nil, now)
	if err != nil {
		return entity.Job{}, err
	}
	return job, s.repository.UpdateJob(ctx, job, expected, nil, nil, &event, result)
}

// GetJob returns authoritative platform job state.
func (s *Service) GetJob(ctx context.Context, input GetJobInput) (entity.Job, error) {
	if input.JobID == uuid.Nil {
		return entity.Job{}, errs.ErrInvalidArgument
	}
	job, err := s.repository.GetJob(ctx, input.JobID)
	if err != nil {
		return entity.Job{}, err
	}
	if err := s.authorizeQuery(ctx, input.Meta, actionJobRead, jobResource(job.ID, job.ProjectID)); err != nil {
		return entity.Job{}, err
	}
	return job, nil
}

// ListJobs returns jobs matching operator or automation filters.
func (s *Service) ListJobs(ctx context.Context, input ListJobsInput) (ListJobsResult, error) {
	if err := s.authorizeQuery(ctx, input.Meta, actionJobList, jobResource(uuid.Nil, input.ProjectID)); err != nil {
		return ListJobsResult{}, err
	}
	filter := query.JobFilter{
		Statuses:      append([]enum.JobStatus(nil), input.Statuses...),
		JobTypes:      append([]enum.JobType(nil), input.JobTypes...),
		ProjectID:     input.ProjectID,
		SlotID:        input.SlotID,
		AgentRunID:    input.AgentRunID,
		ReleaseLineID: input.ReleaseLineID,
		Page:          input.Page,
	}
	jobs, page, err := s.repository.ListJobs(ctx, filter)
	return ListJobsResult{Jobs: jobs, Page: page}, err
}

type finishJobInput struct {
	jobID        uuid.UUID
	leaseToken   string
	status       enum.JobStatus
	eventType    string
	errorCode    string
	errorMessage string
	shortLogTail string
	fullLogRef   string
	nextAction   string
	meta         value.CommandMeta
	operation    string
	action       string
}

func (s *Service) finishJob(ctx context.Context, input finishJobInput) (entity.Job, error) {
	job, err := s.repository.GetJob(ctx, input.jobID)
	if err != nil {
		return entity.Job{}, err
	}
	if err := s.authorizeCommand(ctx, input.meta, input.action, jobResource(job.ID, job.ProjectID)); err != nil {
		return entity.Job{}, err
	}
	if replay, ok, err := s.jobReplay(ctx, input.meta, input.operation, &input.jobID); err != nil || ok {
		return replay, err
	}
	expected, err := expectedVersion(input.meta)
	if err != nil {
		return entity.Job{}, err
	}
	if err := validateActiveJobLease(job, input.leaseToken, s.clock.Now()); err != nil {
		return entity.Job{}, err
	}
	if job.Version != expected || terminalJobStatus(job.Status) {
		return entity.Job{}, errs.ErrConflict
	}
	now := commandTime(input.meta, s.clock.Now())
	previousStatus := string(job.Status)
	job.Status = input.status
	job.LeaseOwner = ""
	job.LeaseTokenHash = ""
	job.LeaseUntil = nil
	job.FinishedAt = timePtr(now)
	job.LastErrorCode = strings.TrimSpace(input.errorCode)
	job.LastErrorMessage = strings.TrimSpace(input.errorMessage)
	job.ShortLogTail = boundedLogTail(input.shortLogTail)
	job.FullLogRef = strings.TrimSpace(input.fullLogRef)
	job.NextAction = strings.TrimSpace(input.nextAction)
	job.UpdatedAt = now
	job.Version = expected + 1
	event, err := s.jobEvent(input.eventType, job, now, payloadPreviousStatus(previousStatus))
	if err != nil {
		return entity.Job{}, err
	}
	result, err := commandResult(input.meta, input.operation, aggregateTypeJob, job.ID, nil, now)
	if err != nil {
		return entity.Job{}, err
	}
	return job, s.repository.UpdateJob(ctx, job, expected, nil, nil, &event, result)
}

type resolvedCreateJobInput struct {
	JobType               enum.JobType
	Priority              enum.JobPriority
	SlotID                *uuid.UUID
	AgentRunID            *uuid.UUID
	ProjectID             *uuid.UUID
	RepositoryID          *uuid.UUID
	ReleaseLineID         *uuid.UUID
	PackageInstallationID *uuid.UUID
	FleetScopeID          *uuid.UUID
	ClusterID             *uuid.UUID
	JobInputJSON          []byte
	PlacementRequest      PlacementResolutionRequest
	PlacementFingerprint  string
}

func (s *Service) resolveJobCreateInput(ctx context.Context, input CreateJobInput) (resolvedCreateJobInput, error) {
	if !validJobType(input.JobType) || !validJobPriority(input.Priority) {
		return resolvedCreateJobInput{}, errs.ErrInvalidArgument
	}
	if _, err := commandIdentity(input.Meta, operationCreateJob); err != nil {
		return resolvedCreateJobInput{}, err
	}
	jobInputJSON, err := normalizedJSONObject(input.JobInputJSON)
	if err != nil {
		return resolvedCreateJobInput{}, err
	}
	if err := validateJobTypeSpecificInput(input, jobInputJSON); err != nil {
		return resolvedCreateJobInput{}, err
	}
	resolved := resolvedCreateJobInput{
		JobType:               input.JobType,
		Priority:              input.Priority,
		SlotID:                input.SlotID,
		AgentRunID:            input.AgentRunID,
		ProjectID:             input.ProjectID,
		RepositoryID:          input.RepositoryID,
		ReleaseLineID:         input.ReleaseLineID,
		PackageInstallationID: input.PackageInstallationID,
		JobInputJSON:          jobInputJSON,
	}
	if input.SlotID != nil {
		slot, err := s.repository.GetSlot(ctx, *input.SlotID)
		if err != nil {
			return resolvedCreateJobInput{}, err
		}
		if input.ProjectID != nil && !sameUUIDPtr(slot.ProjectID, input.ProjectID) {
			return resolvedCreateJobInput{}, errs.ErrConflict
		}
		resolved.ProjectID = slot.ProjectID
		resolved.FleetScopeID = slot.FleetScopeID
		resolved.ClusterID = slot.ClusterID
	} else {
		request, err := jobPlacementRequest(input)
		if err != nil {
			return resolvedCreateJobInput{}, err
		}
		fingerprint, err := placementRequestFingerprint(request)
		if err != nil {
			return resolvedCreateJobInput{}, err
		}
		resolved.PlacementRequest = request
		resolved.PlacementFingerprint = fingerprint
	}
	return resolved, nil
}

func validateJobTypeSpecificInput(input CreateJobInput, jobInputJSON []byte) error {
	switch input.JobType {
	case enum.JobTypeAgentRun:
		if input.AgentRunID == nil || *input.AgentRunID == uuid.Nil {
			return errs.ErrInvalidArgument
		}
		if !bytes.Equal(jobInputJSON, []byte(`{}`)) {
			return errs.ErrInvalidArgument
		}
	}
	return nil
}

func repositoryIDsForJob(repositoryID *uuid.UUID) []uuid.UUID {
	if repositoryID == nil {
		return nil
	}
	return []uuid.UUID{*repositoryID}
}

func jobRuntimeProfile(profile string) string {
	trimmed := strings.TrimSpace(profile)
	if trimmed != "" {
		return trimmed
	}
	return "platform-job"
}

func (s *Service) jobReplay(ctx context.Context, meta value.CommandMeta, operation string, expectedJobID *uuid.UUID) (entity.Job, bool, error) {
	result, replayed, err := s.findCommandResult(ctx, meta, operation, aggregateTypeJob)
	if err != nil || !replayed {
		return entity.Job{}, replayed, err
	}
	if expectedJobID != nil && result.AggregateID != *expectedJobID {
		return entity.Job{}, true, errs.ErrConflict
	}
	job, err := s.repository.GetJob(ctx, result.AggregateID)
	return job, true, err
}

func (s *Service) createJobReplay(ctx context.Context, meta value.CommandMeta) (entity.Job, entity.CommandResult, bool, error) {
	return aggregateReplayWithResult(ctx, meta, operationCreateJob, aggregateTypeJob, s.findCommandResult, s.repository.GetJob)
}

func validateJobReplayScope(job entity.Job, input resolvedCreateJobInput, result entity.CommandResult) error {
	if job.JobType != input.JobType || job.Priority != input.Priority {
		return errs.ErrConflict
	}
	if !sameUUIDPtr(job.SlotID, input.SlotID) ||
		!sameUUIDPtr(job.AgentRunID, input.AgentRunID) ||
		!sameUUIDPtr(job.ProjectID, input.ProjectID) ||
		!sameUUIDPtr(job.RepositoryID, input.RepositoryID) ||
		!sameUUIDPtr(job.ReleaseLineID, input.ReleaseLineID) ||
		!sameUUIDPtr(job.PackageInstallationID, input.PackageInstallationID) ||
		!bytes.Equal(job.JobInputJSON, input.JobInputJSON) {
		return errs.ErrConflict
	}
	if input.FleetScopeID != nil && !sameUUIDPtr(job.FleetScopeID, input.FleetScopeID) {
		return errs.ErrConflict
	}
	if input.ClusterID != nil && !sameUUIDPtr(job.ClusterID, input.ClusterID) {
		return errs.ErrConflict
	}
	if input.SlotID == nil {
		return validatePlacementReplayFingerprint(result, input.PlacementFingerprint)
	}
	return nil
}

func createJobCommandPayload(placementFingerprint string) ([]byte, error) {
	if strings.TrimSpace(placementFingerprint) == "" {
		return nil, nil
	}
	return commandPayloadWithPlacementFingerprint(placementFingerprint)
}

func validateClaimJobInput(input ClaimRunnableJobInput, now time.Time) error {
	if strings.TrimSpace(input.LeaseOwner) == "" || !input.LeaseUntil.After(now) {
		return errs.ErrInvalidArgument
	}
	for _, jobType := range input.JobTypes {
		if !validJobType(jobType) {
			return errs.ErrInvalidArgument
		}
	}
	_, err := commandIdentity(input.Meta, operationClaimJob)
	return err
}

func validateReportJobStepInput(input ReportJobStepProgressInput) error {
	if input.JobID == uuid.Nil || strings.TrimSpace(input.LeaseToken) == "" || strings.TrimSpace(input.StepKey) == "" || !validJobStepStatus(input.Status) {
		return errs.ErrInvalidArgument
	}
	if input.Status == enum.JobStepStatusFailed && strings.TrimSpace(input.ErrorCode) == "" {
		return errs.ErrInvalidArgument
	}
	if len(input.ArtifactRefs) > maxRuntimeArtifactRefs {
		return errs.ErrInvalidArgument
	}
	for _, ref := range input.ArtifactRefs {
		if err := validateRuntimeArtifactRefInput(ref); err != nil {
			return err
		}
	}
	_, err := commandIdentity(input.Meta, operationReportJobStep)
	return err
}

func validateTerminalJobInput(jobID uuid.UUID, leaseToken string, meta value.CommandMeta, operation string) error {
	if jobID == uuid.Nil || strings.TrimSpace(leaseToken) == "" {
		return errs.ErrInvalidArgument
	}
	_, err := commandIdentity(meta, operation)
	return err
}

func validateActiveJobLease(job entity.Job, leaseToken string, now time.Time) error {
	if strings.TrimSpace(job.LeaseOwner) == "" || job.LeaseTokenHash == "" || job.LeaseUntil == nil || !job.LeaseUntil.After(now) {
		return errs.ErrConflict
	}
	if leaseTokenHash(leaseToken) != job.LeaseTokenHash {
		return errs.ErrConflict
	}
	switch job.Status {
	case enum.JobStatusClaimed, enum.JobStatusRunning:
		return nil
	default:
		return errs.ErrConflict
	}
}

func upsertJobStep(job entity.Job, input ReportJobStepProgressInput, now time.Time, newID func() uuid.UUID) entity.JobStep {
	stepKey := strings.TrimSpace(input.StepKey)
	for _, existing := range job.Steps {
		if existing.StepKey == stepKey {
			return updateJobStep(existing, input, now)
		}
	}
	startedAt := input.StartedAt
	if startedAt == nil {
		startedAt = timePtr(now)
	}
	step := entity.JobStep{
		Base: entity.Base{
			ID:        newID(),
			CreatedAt: now,
			UpdatedAt: now,
		},
		JobID:     job.ID,
		StepKey:   stepKey,
		Status:    input.Status,
		StartedAt: startedAt,
	}
	return updateJobStep(step, input, now)
}

func updateJobStep(step entity.JobStep, input ReportJobStepProgressInput, now time.Time) entity.JobStep {
	step.Status = input.Status
	step.ShortLogTail = boundedLogTail(input.ShortLogTail)
	step.ExternalRef = strings.TrimSpace(input.ExternalRef)
	step.ErrorCode = strings.TrimSpace(input.ErrorCode)
	step.ErrorMessage = strings.TrimSpace(input.ErrorMessage)
	if input.StartedAt != nil {
		step.StartedAt = input.StartedAt
	}
	if input.FinishedAt != nil {
		step.FinishedAt = input.FinishedAt
	}
	if terminalJobStepStatus(input.Status) && step.FinishedAt == nil {
		step.FinishedAt = timePtr(now)
	}
	if step.Version > 0 {
		step.Version++
	} else {
		step.Version = 1
	}
	step.UpdatedAt = now
	return step
}

func updateJobAfterStep(job *entity.Job, input ReportJobStepProgressInput, now time.Time) {
	job.Status = enum.JobStatusRunning
	if job.StartedAt == nil {
		job.StartedAt = timePtr(now)
	}
	job.ShortLogTail = boundedLogTail(input.ShortLogTail)
	if input.Status == enum.JobStepStatusFailed {
		job.LastErrorCode = strings.TrimSpace(input.ErrorCode)
		job.LastErrorMessage = strings.TrimSpace(input.ErrorMessage)
		job.NextAction = defaultJobFailureAction
	}
	job.UpdatedAt = now
	job.Version++
}

func replaceJobStep(steps []entity.JobStep, step entity.JobStep) []entity.JobStep {
	result := append([]entity.JobStep(nil), steps...)
	for index := range result {
		if result[index].StepKey == step.StepKey {
			result[index] = step
			return result
		}
	}
	return append(result, step)
}

func newRuntimeArtifactRefs(ids runtimerepo.IDGenerator, inputs []RuntimeArtifactRefInput, jobID *uuid.UUID, slotID *uuid.UUID, now time.Time) ([]entity.RuntimeArtifactRef, error) {
	refs := make([]entity.RuntimeArtifactRef, 0, len(inputs))
	for _, input := range inputs {
		if err := validateRuntimeArtifactRefInput(input); err != nil {
			return nil, err
		}
		refs = append(refs, entity.RuntimeArtifactRef{
			ID:           ids.New(),
			JobID:        jobID,
			SlotID:       slotID,
			ArtifactType: input.ArtifactType,
			ExternalRef:  strings.TrimSpace(input.ExternalRef),
			Digest:       strings.TrimSpace(input.Digest),
			MetadataJSON: normalizedMetadataJSON(input.MetadataJSON),
			CreatedAt:    now,
		})
	}
	return refs, nil
}

func validateRuntimeArtifactRefInput(input RuntimeArtifactRefInput) error {
	if !validRuntimeArtifactType(input.ArtifactType) || strings.TrimSpace(input.ExternalRef) == "" {
		return errs.ErrInvalidArgument
	}
	_, err := normalizedJSONObject(input.MetadataJSON)
	return err
}

func normalizedJSONObject(payload []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return []byte(`{}`), nil
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &parsed); err != nil {
		return nil, errs.ErrInvalidArgument
	}
	if parsed == nil {
		return nil, errs.ErrInvalidArgument
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, trimmed); err != nil {
		return nil, errs.ErrInvalidArgument
	}
	return compact.Bytes(), nil
}

func normalizedMetadataJSON(payload []byte) []byte {
	normalized, err := normalizedJSONObject(payload)
	if err != nil {
		return []byte(`{}`)
	}
	return normalized
}

func requestedBy(actor value.Actor) *uuid.UUID {
	id, err := uuid.Parse(strings.TrimSpace(actor.ID))
	if err != nil || id == uuid.Nil {
		return nil
	}
	return &id
}

func boundedLogTail(text string) string {
	text = strings.TrimSpace(text)
	if len(text) <= maxShortLogTailBytes {
		return strings.ToValidUTF8(text, "")
	}
	tail := text[len(text)-maxShortLogTailBytes:]
	for len(tail) > 0 && !utf8.ValidString(tail) {
		_, size := utf8.DecodeRuneInString(tail)
		if size < 1 {
			return ""
		}
		tail = tail[size:]
	}
	return strings.ToValidUTF8(tail, "")
}

func leaseTokenHash(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func validJobType(jobType enum.JobType) bool {
	switch jobType {
	case enum.JobTypeMirror, enum.JobTypeBuild, enum.JobTypeDeploy, enum.JobTypeCleanup, enum.JobTypeHealthCheck, enum.JobTypeHousekeeping, enum.JobTypeWorkspaceMaterialization, enum.JobTypeAgentRun:
		return true
	default:
		return false
	}
}

func validJobPriority(priority enum.JobPriority) bool {
	switch priority {
	case enum.JobPriorityLow, enum.JobPriorityNormal, enum.JobPriorityHigh, enum.JobPriorityBlocking:
		return true
	default:
		return false
	}
}

func validJobStepStatus(status enum.JobStepStatus) bool {
	switch status {
	case enum.JobStepStatusPending, enum.JobStepStatusRunning, enum.JobStepStatusSucceeded, enum.JobStepStatusFailed, enum.JobStepStatusSkipped:
		return true
	default:
		return false
	}
}

func validRuntimeArtifactType(artifactType enum.RuntimeArtifactType) bool {
	switch artifactType {
	case enum.RuntimeArtifactTypeImageRef, enum.RuntimeArtifactTypeKubernetesJob, enum.RuntimeArtifactTypeNamespace, enum.RuntimeArtifactTypeDeployment, enum.RuntimeArtifactTypeLogRef, enum.RuntimeArtifactTypeManifestRef:
		return true
	default:
		return false
	}
}

func terminalJobStatus(status enum.JobStatus) bool {
	switch status {
	case enum.JobStatusSucceeded, enum.JobStatusFailed, enum.JobStatusCancelled, enum.JobStatusTimedOut:
		return true
	default:
		return false
	}
}

func terminalJobStepStatus(status enum.JobStepStatus) bool {
	switch status {
	case enum.JobStepStatusSucceeded, enum.JobStepStatusFailed, enum.JobStepStatusSkipped:
		return true
	default:
		return false
	}
}

func timePtr(value time.Time) *time.Time {
	return &value
}
