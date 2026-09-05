package platform

import (
	"context"
	_ "embed"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/environment_impact_target.sql
var queryEnvironmentImpactTarget string

//go:embed sql/environment_impact_consumers.sql
var queryEnvironmentImpactConsumers string

func (repository *Repository) environmentImpactTarget(ctx context.Context, tx pgx.Tx, current scope, ref, version string) (entity.RuntimeEnvironmentImpact, string, error) {
	var result entity.RuntimeEnvironmentImpact
	var projectRef, projectID string
	err := tx.QueryRow(ctx, queryEnvironmentImpactTarget, current.organizationID, ref, version).Scan(
		&result.EnvironmentRef, &result.EnvironmentVersion, &result.TargetVersionRef, &result.TargetDigest, &projectRef, &projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, "", errs.ErrNotFound
	}
	if err != nil {
		return result, "", errs.ErrUnavailable
	}
	if current.authorityProjectID != "" && current.authorityProjectID != projectID {
		return result, "", errs.ErrForbidden
	}
	if err := repository.requireAccess(ctx, tx, current, "project.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "PROJECT", ResourceRef: projectRef}); err != nil {
		return result, "", err
	}
	return result, projectRef, nil
}

func (repository *Repository) GetRuntimeEnvironmentImpact(ctx context.Context, principal value.Principal, ref, version, search string, page query.Page) (entity.RuntimeEnvironmentImpact, error) {
	search = strings.TrimSpace(search)
	if !utf8.ValidString(search) || utf8.RuneCountInString(search) > 200 || strings.ContainsRune(search, 0) {
		return entity.RuntimeEnvironmentImpact{}, errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.RuntimeEnvironmentImpact{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return entity.RuntimeEnvironmentImpact{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, _, err := repository.environmentImpactTarget(ctx, tx, current, ref, version)
	if err != nil {
		return result, err
	}
	filter := query.Filter{ResourceRef: ref, Query: search, Category: result.TargetVersionRef, Page: page}
	cursor, err := decodeCatalogCursor(current, "ENVIRONMENT_IMPACT", filter)
	if err != nil {
		return result, err
	}
	limit := boundedPage(page)
	rows, err := tx.Query(ctx, queryEnvironmentImpactConsumers, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID, "environment_ref": ref, "query": search,
		"target_ref": result.TargetVersionRef, "evaluated_at": time.Now().UTC(), "cursor_ref": cursor, "page_size": limit + 1,
	})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	for rows.Next() {
		var item entity.RuntimeEnvironmentConsumer
		if err := rows.Scan(&item.AgentRef, &item.AgentVersion, &item.BindingRef, &item.BindingVersion, &item.VersionRef, &item.ProjectRef, &result.Total); err != nil {
			rows.Close()
			return result, errs.ErrUnavailable
		}
		if item.AgentRef != "" {
			result.Consumers = append(result.Consumers, item)
		}
	}
	rows.Close()
	if rows.Err() != nil {
		return result, errs.ErrUnavailable
	}
	if len(result.Consumers) > int(limit) {
		result.Consumers = result.Consumers[:limit]
		result.NextPageToken = encodeCatalogCursor(current, "ENVIRONMENT_IMPACT", filter, result.Consumers[len(result.Consumers)-1].AgentRef)
	}
	if err := tx.Commit(ctx); err != nil {
		return result, errs.ErrUnavailable
	}
	return result, nil
}

func (repository *Repository) rebindRuntimeEnvironment(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.RuntimeEnvironmentRebindInput)
	if !ok || payload.VersionRef == "" || input.Mutation.ExpectedVersion == nil || len(payload.Consumers) == 0 || len(payload.Consumers) > 100 {
		return commandOutcome{}, errs.ErrInvalid
	}
	var environmentID, projectID, lockedProjectRef, currentRevisionID string
	var lockedVersion, currentRevision int64
	err := tx.QueryRow(ctx, queryRuntimeConfigurationLockEnvironment, current.organizationID, payload.EnvironmentRef).Scan(
		&environmentID, &projectID, &lockedProjectRef, &lockedVersion, &currentRevisionID, &currentRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrNotFound
	}
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	target, projectRef, err := repository.environmentImpactTarget(ctx, tx, current, payload.EnvironmentRef, payload.VersionRef)
	if err != nil {
		return commandOutcome{}, err
	}
	if target.EnvironmentVersion != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	consumers := append([]entity.RuntimeEnvironmentConsumer(nil), payload.Consumers...)
	sort.Slice(consumers, func(i, j int) bool { return consumers[i].AgentRef < consumers[j].AgentRef })
	result := command.Result{}
	for i, consumer := range consumers {
		if consumer.AgentRef == "" || consumer.BindingRef == "" || consumer.VersionRef == "" || consumer.AgentVersion < 1 || consumer.BindingVersion < 1 ||
			(i > 0 && consumers[i-1].AgentRef == consumer.AgentRef) {
			return commandOutcome{}, errs.ErrInvalid
		}
		if err := repository.requireAccess(ctx, tx, current, "agent.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "AGENT", ResourceRef: consumer.AgentRef}); err != nil {
			return commandOutcome{}, err
		}
		if _, err := repository.lockRuntimeAgent(ctx, tx, current, consumer.AgentRef); err != nil {
			return commandOutcome{}, err
		}
		view, err := repository.getRuntimeConfigurationViewTx(ctx, tx, current, consumer.AgentRef)
		if err != nil {
			return commandOutcome{}, err
		}
		binding := view.EnvironmentBinding
		if view.AgentVersion != consumer.AgentVersion || binding.Ref != consumer.BindingRef || binding.Version != consumer.BindingVersion ||
			binding.VersionRef != consumer.VersionRef || binding.EnvironmentRef != payload.EnvironmentRef {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		nested := input
		nested.Kind = command.BindAgentRuntimeEnvironment
		nested.Mutation.ExpectedVersion = &consumer.AgentVersion
		nested.Payload = command.RuntimeEnvironmentBindingInput{AgentRef: consumer.AgentRef, EnvironmentRef: payload.EnvironmentRef, VersionRef: payload.VersionRef}
		outcome, err := repository.bindRuntimeEnvironment(ctx, tx, current, nested)
		if err != nil {
			return commandOutcome{}, err
		}
		if err := repository.emitCommandOutcomePlatformEvent(ctx, tx, current, outcome); err != nil {
			return commandOutcome{}, err
		}
		result.EnvironmentBindings = append(result.EnvironmentBindings, outcome.result.RuntimeConfiguration.EnvironmentBinding)
	}
	return commandOutcome{result: result, resourceKind: "RUNTIME_ENVIRONMENT", resourceRef: payload.EnvironmentRef,
		projectRef: projectRef, summary: "i18n:RUNTIME_ENVIRONMENT_SELECTED_REBOUND"}, nil
}
