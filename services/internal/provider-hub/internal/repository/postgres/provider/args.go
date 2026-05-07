package provider

import (
	"github.com/jackc/pgx/v5"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
)

const (
	defaultPageSize = int32(100)
	maxPageSize     = int32(500)
)

type pageQueryArgs struct {
	args       pgx.NamedArgs
	limit      int32
	nextOffset int32
}

func accountRuntimeStateArgs(state entity.ProviderAccountRuntimeState) pgx.NamedArgs {
	return withBaseArgs(state.Base, pgx.NamedArgs{
		"external_account_id": state.ExternalAccountID,
		"provider_slug":       string(state.ProviderSlug),
		"status":              string(state.Status),
		"last_checked_at":     postgreslib.NullableTime(state.LastCheckedAt),
		"last_success_at":     postgreslib.NullableTime(state.LastSuccessAt),
		"last_error_code":     state.LastErrorCode,
		"last_error_message":  state.LastErrorMessage,
	})
}

func accountRuntimeStateLookupArgs(lookup query.AccountRuntimeStateLookup) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                  postgreslib.NullableUUID(lookup.ID),
		"external_account_id": postgreslib.NullableUUID(lookup.ExternalAccountID),
		"provider_slug":       string(lookup.ProviderSlug),
	}
}

func accountRuntimeStateFilterArgs(filter query.AccountRuntimeStateFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"provider_slug":        string(filter.ProviderSlug),
		"external_account_ids": postgreslib.UUIDValues(filter.ExternalAccountIDs),
		"statuses":             postgreslib.StringValues(filter.Statuses),
	})
}

func limitSnapshotArgs(snapshot entity.ProviderLimitSnapshot) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                  snapshot.ID,
		"external_account_id": snapshot.ExternalAccountID,
		"provider_slug":       string(snapshot.ProviderSlug),
		"limit_class":         snapshot.LimitClass,
		"remaining":           int64PtrValue(snapshot.Remaining),
		"limit_value":         int64PtrValue(snapshot.LimitValue),
		"reset_at":            postgreslib.NullableTime(snapshot.ResetAt),
		"captured_at":         snapshot.CapturedAt,
		"source":              string(snapshot.Source),
	}
}

func limitSnapshotFilterArgs(filter query.LimitSnapshotFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"external_account_id": postgreslib.NullableUUID(filter.ExternalAccountID),
		"provider_slug":       string(filter.ProviderSlug),
		"limit_classes":       filter.LimitClasses,
		"captured_since":      postgreslib.NullableTime(filter.CapturedSince),
	})
}

func providerOperationArgs(operation entity.ProviderOperation) pgx.NamedArgs {
	return withBaseArgs(operation.Base, pgx.NamedArgs{
		"command_id":             operation.CommandID,
		"actor_id":               postgreslib.NullableUUID(operation.ActorID),
		"external_account_id":    operation.ExternalAccountID,
		"provider_slug":          string(operation.ProviderSlug),
		"operation_type":         string(operation.OperationType),
		"target_ref":             operation.TargetRef,
		"status":                 string(operation.Status),
		"result_ref":             operation.ResultRef,
		"error_code":             operation.ErrorCode,
		"error_message":          operation.ErrorMessage,
		"rate_limit_snapshot_id": postgreslib.NullableUUID(operation.RateLimitSnapshotID),
		"started_at":             operation.StartedAt,
		"finished_at":            postgreslib.NullableTime(operation.FinishedAt),
	})
}

func providerOperationFilterArgs(filter query.ProviderOperationFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"provider_slug":       string(filter.ProviderSlug),
		"external_account_id": postgreslib.NullableUUID(filter.ExternalAccountID),
		"operation_types":     postgreslib.StringValues(filter.OperationTypes),
		"statuses":            postgreslib.StringValues(filter.Statuses),
		"target_ref":          filter.TargetRef,
		"started_since":       postgreslib.NullableTime(filter.StartedSince),
	})
}

func withPage(page value.PageRequest, args pgx.NamedArgs) pageQueryArgs {
	limit, offset, nextOffset := postgreslib.OffsetPageBounds(page.PageSize, page.PageToken, defaultPageSize, maxPageSize)
	args["limit"] = limit + 1
	args["offset"] = offset
	return pageQueryArgs{args: args, limit: limit, nextOffset: nextOffset}
}

func pageResult[T any](items []T, limit int32, nextOffset int32) ([]T, value.PageResult) {
	pageItems, token := postgreslib.TrimOffsetPage(items, limit, nextOffset)
	return pageItems, value.PageResult{NextPageToken: token}
}

func withBaseArgs(base entity.Base, args pgx.NamedArgs) pgx.NamedArgs {
	return postgreslib.AddBaseArgs(args, base.ID, base.Version, base.CreatedAt, base.UpdatedAt)
}

func int64PtrValue(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}
