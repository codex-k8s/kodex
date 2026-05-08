package runtime

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/query"
)

const (
	defaultRuntimeArtifactRefPageSize = int32(50)
	maxRuntimeArtifactRefPageSize     = int32(200)
)

// RecordRuntimeArtifactRef stores one reference to an external runtime artifact.
func (r *Repository) RecordRuntimeArtifactRef(ctx context.Context, ref entity.RuntimeArtifactRef, result entity.CommandResult) error {
	err := postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		return postgreslib.RunDistinctMutations(
			ctx,
			tx,
			errs.ErrConflict,
			postgreslib.Mutation{Query: queryRuntimeArtifactRefInsert, Args: runtimeArtifactRefArgs(ref), RequireAffected: true},
			postgreslib.Mutation{Query: queryCommandResultInsert, Args: commandResultArgs(result), RequireAffected: true},
		)
	})
	return wrapError(operationRecordRuntimeArtifactRef, err)
}

// GetRuntimeArtifactRef returns one external runtime artifact reference by id.
func (r *Repository) GetRuntimeArtifactRef(ctx context.Context, id uuid.UUID) (entity.RuntimeArtifactRef, error) {
	return getByID(ctx, r.db, id, queryRuntimeArtifactRefGet, operationGetRuntimeArtifactRef, scanRuntimeArtifactRef)
}

// ListRuntimeArtifactRefs returns external runtime artifact references matching the filter and page.
func (r *Repository) ListRuntimeArtifactRefs(ctx context.Context, filter query.RuntimeArtifactRefFilter) ([]entity.RuntimeArtifactRef, query.PageResult, error) {
	limit, offset, nextOffset := postgreslib.OffsetPageBounds(filter.Page.PageSize, filter.Page.PageToken, defaultRuntimeArtifactRefPageSize, maxRuntimeArtifactRefPageSize)
	rows, err := r.db.Query(ctx, queryRuntimeArtifactRefList, pgx.NamedArgs{
		"job_id":         postgreslib.NullableUUID(filter.JobID),
		"slot_id":        postgreslib.NullableUUID(filter.SlotID),
		"artifact_types": postgreslib.StringValues(filter.ArtifactTypes),
		"limit":          limit + 1,
		"offset":         offset,
	})
	if err != nil {
		return nil, query.PageResult{}, wrapError(operationListRuntimeArtifactRefs, err)
	}
	refs, err := postgreslib.ScanRows(rows, scanRuntimeArtifactRef)
	if err != nil {
		return nil, query.PageResult{}, wrapError(operationListRuntimeArtifactRefs, err)
	}
	refs, nextToken := postgreslib.TrimOffsetPage(refs, limit, nextOffset)
	return refs, query.PageResult{NextPageToken: nextToken}, nil
}

func runtimeArtifactRefArgs(ref entity.RuntimeArtifactRef) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":            ref.ID,
		"job_id":        postgreslib.NullableUUID(ref.JobID),
		"slot_id":       postgreslib.NullableUUID(ref.SlotID),
		"artifact_type": string(ref.ArtifactType),
		"external_ref":  ref.ExternalRef,
		"digest":        ref.Digest,
		"metadata_json": postgreslib.JSONPayload(ref.MetadataJSON),
		"created_at":    ref.CreatedAt,
	}
}
