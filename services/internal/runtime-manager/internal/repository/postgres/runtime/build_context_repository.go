package runtime

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	runtimerepo "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/repository/runtime"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
)

// PrepareBuildContext stores or reuses a build context request and records command idempotency.
func (r *Repository) PrepareBuildContext(ctx context.Context, buildContext entity.BuildContext, resultFactory runtimerepo.BuildContextCommandResultFactory) (entity.BuildContext, error) {
	var stored entity.BuildContext
	err := postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		existing, err := queryOne(ctx, tx, queryBuildContextGetByFingerprint, pgx.NamedArgs{"context_fingerprint": buildContext.ContextFingerprint}, scanBuildContext)
		switch {
		case err == nil:
			result, err := resultFactory(existing)
			if err != nil {
				return err
			}
			if err := postgreslib.RunMutation(ctx, tx, errs.ErrConflict, postgreslib.Mutation{Query: queryCommandResultInsert, Args: commandResultArgs(result), RequireAffected: true}); err != nil {
				return err
			}
			stored = existing
			return nil
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}
		result, err := resultFactory(buildContext)
		if err != nil {
			return err
		}
		args, err := buildContextArgs(buildContext)
		if err != nil {
			return err
		}
		if err := postgreslib.RunDistinctMutations(
			ctx,
			tx,
			errs.ErrConflict,
			postgreslib.Mutation{Query: queryBuildContextInsert, Args: args, RequireAffected: true},
			postgreslib.Mutation{Query: queryCommandResultInsert, Args: commandResultArgs(result), RequireAffected: true},
		); err != nil {
			return err
		}
		stored = buildContext
		return nil
	})
	return stored, wrapError(operationPrepareBuildContext, err)
}

// UpdateBuildContext stores build context progress and command result atomically.
func (r *Repository) UpdateBuildContext(ctx context.Context, buildContext entity.BuildContext, previousVersion int64, result entity.CommandResult) error {
	args, err := buildContextArgs(buildContext)
	if err != nil {
		return wrapError(operationUpdateBuildContext, err)
	}
	args["previous_version"] = previousVersion
	return r.mutateRecordWithCommandResult(ctx, operationUpdateBuildContext, queryBuildContextUpdate, args, result)
}

// GetBuildContext returns one build context by id.
func (r *Repository) GetBuildContext(ctx context.Context, id uuid.UUID) (entity.BuildContext, error) {
	return r.getBuildContext(ctx, operationGetBuildContext, queryBuildContextGet, pgx.NamedArgs{"id": id})
}

// GetBuildContextByFingerprint returns one build context by deterministic context fingerprint.
func (r *Repository) GetBuildContextByFingerprint(ctx context.Context, fingerprint string) (entity.BuildContext, error) {
	return r.getBuildContext(ctx, operationGetBuildContextByFingerprint, queryBuildContextGetByFingerprint, pgx.NamedArgs{"context_fingerprint": fingerprint})
}

func (r *Repository) getBuildContext(ctx context.Context, operation string, sql string, args pgx.NamedArgs) (entity.BuildContext, error) {
	buildContext, err := queryOne(ctx, r.db, sql, args, scanBuildContext)
	return buildContext, wrapError(operation, err)
}

func buildContextArgs(buildContext entity.BuildContext) (pgx.NamedArgs, error) {
	affectedServiceKeysJSON, err := json.Marshal(buildContext.AffectedServiceKeys)
	if err != nil {
		return nil, err
	}
	return pgx.NamedArgs{
		"id":                         buildContext.ID,
		"status":                     string(buildContext.Status),
		"project_id":                 buildContext.ProjectID,
		"repository_id":              buildContext.RepositoryID,
		"provider":                   buildContext.Provider,
		"provider_owner":             buildContext.ProviderOwner,
		"provider_name":              buildContext.ProviderName,
		"source_ref":                 buildContext.SourceRef,
		"source_commit_sha":          buildContext.SourceCommitSHA,
		"affected_service_keys_json": string(affectedServiceKeysJSON),
		"build_plan_fingerprint":     buildContext.BuildPlanFingerprint,
		"context_fingerprint":        buildContext.ContextFingerprint,
		"source_snapshot_ref":        buildContext.SourceSnapshotRef,
		"source_snapshot_digest":     buildContext.SourceSnapshotDigest,
		"build_context_ref":          buildContext.BuildContextRef,
		"build_context_digest":       buildContext.BuildContextDigest,
		"started_at":                 postgreslib.NullableTime(buildContext.StartedAt),
		"finished_at":                postgreslib.NullableTime(buildContext.FinishedAt),
		"last_error_code":            buildContext.LastErrorCode,
		"last_error_message":         buildContext.LastErrorMessage,
		"next_action":                buildContext.NextAction,
		"version":                    buildContext.Version,
		"created_at":                 buildContext.CreatedAt,
		"updated_at":                 buildContext.UpdatedAt,
	}, nil
}
