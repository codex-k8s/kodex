package runtime

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	runtimerepo "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/repository/runtime"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/query"
)

const (
	defaultJobPageSize = int32(50)
	maxJobPageSize     = int32(200)
)

// CreateJob stores a new platform job, its event and command result atomically.
func (r *Repository) CreateJob(ctx context.Context, job entity.Job, event entity.OutboxEvent, result entity.CommandResult) error {
	err := postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		return postgreslib.RunDistinctMutations(
			ctx,
			tx,
			errs.ErrConflict,
			postgreslib.Mutation{Query: queryJobInsert, Args: jobArgs(job), RequireAffected: true},
			postgreslib.Mutation{Query: queryOutboxEventInsert, Args: outboxEventArgs(event), RequireAffected: true},
			postgreslib.Mutation{Query: queryCommandResultInsert, Args: commandResultArgs(result), RequireAffected: true},
		)
	})
	return wrapError(operationCreateJob, err)
}

// ClaimRunnableJob atomically leases one runnable job and stores its start event.
func (r *Repository) ClaimRunnableJob(ctx context.Context, filter query.JobClaimFilter, eventFactory runtimerepo.JobEventFactory) (entity.Job, error) {
	var claimed entity.Job
	err := postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		job, err := queryOne(ctx, tx, queryJobClaim, jobClaimArgs(filter), scanJob)
		if err != nil {
			return err
		}
		event, err := eventFactory(job)
		if err != nil {
			return err
		}
		if err := postgreslib.RunMutation(ctx, tx, errs.ErrConflict, postgreslib.Mutation{
			Query:           queryOutboxEventInsert,
			Args:            outboxEventArgs(event),
			RequireAffected: true,
		}); err != nil {
			return err
		}
		claimed = job
		return nil
	})
	return claimed, wrapError(operationClaimRunnableJob, err)
}

// UpdateJob stores a job mutation, changed steps, artifact refs, optional event and command result atomically.
func (r *Repository) UpdateJob(
	ctx context.Context,
	job entity.Job,
	previousVersion int64,
	steps []entity.JobStep,
	refs []entity.RuntimeArtifactRef,
	event *entity.OutboxEvent,
	result entity.CommandResult,
) error {
	err := postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		if err := postgreslib.RunMutation(ctx, tx, errs.ErrConflict, postgreslib.Mutation{
			Query:           queryJobUpdate,
			Args:            jobUpdateArgs(job, previousVersion),
			RequireAffected: true,
		}); err != nil {
			return err
		}
		for _, step := range steps {
			if err := postgreslib.RunMutation(ctx, tx, errs.ErrConflict, postgreslib.Mutation{Query: queryJobStepUpsert, Args: jobStepArgs(step), RequireAffected: true}); err != nil {
				return err
			}
		}
		for _, ref := range refs {
			if err := postgreslib.RunMutation(ctx, tx, errs.ErrConflict, postgreslib.Mutation{Query: queryRuntimeArtifactRefInsert, Args: runtimeArtifactRefArgs(ref), RequireAffected: true}); err != nil {
				return err
			}
		}
		if event != nil {
			if err := postgreslib.RunMutation(ctx, tx, errs.ErrConflict, postgreslib.Mutation{Query: queryOutboxEventInsert, Args: outboxEventArgs(*event), RequireAffected: true}); err != nil {
				return err
			}
		}
		return postgreslib.RunMutation(ctx, tx, errs.ErrConflict, postgreslib.Mutation{Query: queryCommandResultInsert, Args: commandResultArgs(result), RequireAffected: true})
	})
	return wrapError(operationUpdateJob, err)
}

// GetJob returns one platform job by id.
func (r *Repository) GetJob(ctx context.Context, id uuid.UUID) (entity.Job, error) {
	job, err := queryOne(ctx, r.db, queryJobGet, pgx.NamedArgs{"id": id}, scanJob)
	if err != nil {
		return entity.Job{}, wrapError(operationGetJob, err)
	}
	jobs := []entity.Job{job}
	if err := r.loadJobSteps(ctx, jobs); err != nil {
		return entity.Job{}, wrapError(operationGetJob, err)
	}
	return jobs[0], nil
}

// ListJobs returns platform jobs matching the filter and page.
func (r *Repository) ListJobs(ctx context.Context, filter query.JobFilter) ([]entity.Job, query.PageResult, error) {
	limit, offset, nextOffset := postgreslib.OffsetPageBounds(filter.Page.PageSize, filter.Page.PageToken, defaultJobPageSize, maxJobPageSize)
	rows, err := r.db.Query(ctx, queryJobList, pgx.NamedArgs{
		"statuses":        postgreslib.StringValues(filter.Statuses),
		"job_types":       postgreslib.StringValues(filter.JobTypes),
		"project_id":      postgreslib.NullableUUID(filter.ProjectID),
		"slot_id":         postgreslib.NullableUUID(filter.SlotID),
		"agent_run_id":    postgreslib.NullableUUID(filter.AgentRunID),
		"release_line_id": postgreslib.NullableUUID(filter.ReleaseLineID),
		"limit":           limit + 1,
		"offset":          offset,
	})
	if err != nil {
		return nil, query.PageResult{}, wrapError(operationListJobs, err)
	}
	jobs, err := postgreslib.ScanRows(rows, scanJob)
	if err != nil {
		return nil, query.PageResult{}, wrapError(operationListJobs, err)
	}
	jobs, nextToken := postgreslib.TrimOffsetPage(jobs, limit, nextOffset)
	if err := r.loadJobSteps(ctx, jobs); err != nil {
		return nil, query.PageResult{}, wrapError(operationListJobs, err)
	}
	return jobs, query.PageResult{NextPageToken: nextToken}, nil
}

func (r *Repository) loadJobSteps(ctx context.Context, jobs []entity.Job) error {
	if len(jobs) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(jobs))
	for _, job := range jobs {
		ids = append(ids, job.ID)
	}
	rows, err := r.db.Query(ctx, queryJobStepListByJobIDs, pgx.NamedArgs{"job_ids": postgreslib.UUIDValues(ids)})
	if err != nil {
		return err
	}
	steps, err := postgreslib.ScanRows(rows, scanJobStep)
	if err != nil {
		return err
	}
	byJob := make(map[uuid.UUID][]entity.JobStep, len(jobs))
	for _, step := range steps {
		byJob[step.JobID] = append(byJob[step.JobID], step)
	}
	for index := range jobs {
		jobs[index].Steps = byJob[jobs[index].ID]
	}
	return nil
}

func jobClaimArgs(filter query.JobClaimFilter) pgx.NamedArgs {
	return pgx.NamedArgs{
		"job_types":        postgreslib.StringValues(filter.JobTypes),
		"fleet_scope_id":   postgreslib.NullableUUID(filter.FleetScopeID),
		"lease_owner":      filter.LeaseOwner,
		"lease_token_hash": filter.LeaseTokenHash,
		"lease_until":      filter.LeaseUntil,
		"now":              filter.Now,
	}
}

func jobUpdateArgs(job entity.Job, previousVersion int64) pgx.NamedArgs {
	args := jobArgs(job)
	args["previous_version"] = previousVersion
	return args
}

func jobArgs(job entity.Job) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                      job.ID,
		"command_id":              job.CommandID,
		"job_type":                string(job.JobType),
		"status":                  string(job.Status),
		"priority":                string(job.Priority),
		"job_input_json":          postgreslib.JSONPayload(job.JobInputJSON),
		"lease_owner":             job.LeaseOwner,
		"lease_token_hash":        job.LeaseTokenHash,
		"lease_until":             postgreslib.NullableTime(job.LeaseUntil),
		"claim_attempt":           job.ClaimAttempt,
		"slot_id":                 postgreslib.NullableUUID(job.SlotID),
		"agent_run_id":            postgreslib.NullableUUID(job.AgentRunID),
		"project_id":              postgreslib.NullableUUID(job.ProjectID),
		"repository_id":           postgreslib.NullableUUID(job.RepositoryID),
		"release_line_id":         postgreslib.NullableUUID(job.ReleaseLineID),
		"package_installation_id": postgreslib.NullableUUID(job.PackageInstallationID),
		"fleet_scope_id":          postgreslib.NullableUUID(job.FleetScopeID),
		"cluster_id":              postgreslib.NullableUUID(job.ClusterID),
		"requested_by":            postgreslib.NullableUUID(job.RequestedBy),
		"created_at":              job.CreatedAt,
		"started_at":              postgreslib.NullableTime(job.StartedAt),
		"finished_at":             postgreslib.NullableTime(job.FinishedAt),
		"next_action":             job.NextAction,
		"last_error_code":         job.LastErrorCode,
		"last_error_message":      job.LastErrorMessage,
		"short_log_tail":          job.ShortLogTail,
		"full_log_ref":            job.FullLogRef,
		"updated_at":              job.UpdatedAt,
		"version":                 job.Version,
	}
}

func jobStepArgs(step entity.JobStep) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":             step.ID,
		"job_id":         step.JobID,
		"step_key":       step.StepKey,
		"status":         string(step.Status),
		"started_at":     postgreslib.NullableTime(step.StartedAt),
		"finished_at":    postgreslib.NullableTime(step.FinishedAt),
		"short_log_tail": step.ShortLogTail,
		"external_ref":   step.ExternalRef,
		"error_code":     step.ErrorCode,
		"error_message":  step.ErrorMessage,
		"version":        step.Version,
		"created_at":     step.CreatedAt,
		"updated_at":     step.UpdatedAt,
	}
}
