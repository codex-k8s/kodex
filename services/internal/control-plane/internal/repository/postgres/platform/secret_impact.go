package platform

import (
	"context"
	_ "embed"
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/secret_impact_target.sql
var querySecretImpactTarget string

//go:embed sql/secret_impact_consumers.sql
var querySecretImpactConsumers string

//go:embed sql/secret_rebind_source_version.sql
var querySecretRebindSourceVersion string

//go:embed sql/runtime_secret_revision_retained.sql
var queryRuntimeSecretRevisionRetained string

//go:embed sql/runtime_secret_retire_revision.sql
var queryRuntimeSecretRetireRevision string

func (repository *Repository) secretImpactTarget(ctx context.Context, tx pgx.Tx, current scope, ref string, revision int64) (entity.RuntimeSecretImpact, string, error) {
	var result entity.RuntimeSecretImpact
	if revision < 0 {
		return result, "", errs.ErrInvalid
	}
	var projectRef, projectID string
	err := tx.QueryRow(ctx, querySecretImpactTarget, current.organizationID, ref, revision).Scan(&result.SecretRef, &result.SecretVersion, &result.TargetRevision, &projectRef, &projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, "", errs.ErrNotFound
	}
	if err != nil {
		return result, "", errs.ErrUnavailable
	}
	if current.authorityProjectID != "" && current.authorityProjectID != projectID {
		return result, "", errs.ErrForbidden
	}
	if err := repository.requireAccess(ctx, tx, current, "secret.rotate", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "SECRET", ResourceRef: ref}); err != nil {
		return result, "", err
	}
	return result, projectRef, nil
}

func (repository *Repository) GetRuntimeSecretImpact(ctx context.Context, principal value.Principal, ref string, revision int64, page query.Page) (entity.RuntimeSecretImpact, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.RuntimeSecretImpact{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return entity.RuntimeSecretImpact{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, _, err := repository.secretImpactTarget(ctx, tx, current, ref, revision)
	if err != nil {
		return result, err
	}
	filter := query.Filter{Query: ref, Category: strconv.FormatInt(result.TargetRevision, 10), Page: page}
	cursor, err := decodeCatalogCursor(current, "SECRET_IMPACT", filter)
	if err != nil {
		return result, err
	}
	limit := boundedPage(page)
	rows, err := tx.Query(ctx, querySecretImpactConsumers, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID,
		"authority_project": current.authorityProjectID, "secret_ref": ref, "target_revision": result.TargetRevision, "evaluated_at": time.Now().UTC(), "cursor_ref": cursor, "page_size": limit + 1})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	var cursors []string
	for rows.Next() {
		var item entity.RuntimeSecretImpactConsumer
		var key string
		if err := rows.Scan(&key, &item.EnvironmentRef, &item.EnvironmentVersion, &item.EnvironmentVersionRef, &item.SecretRevisions,
			&item.Consumer.AgentRef, &item.Consumer.AgentVersion, &item.Consumer.BindingRef, &item.Consumer.BindingVersion, &item.Consumer.ProjectRef, &result.Total); err != nil {
			rows.Close()
			return result, errs.ErrUnavailable
		}
		if key != "" {
			item.Consumer.VersionRef = item.EnvironmentVersionRef
			result.Consumers = append(result.Consumers, item)
			cursors = append(cursors, key)
		}
	}
	rows.Close()
	if rows.Err() != nil {
		return result, errs.ErrUnavailable
	}
	if len(result.Consumers) > int(limit) {
		result.Consumers = result.Consumers[:limit]
		result.NextPageToken = encodeCatalogCursor(current, "SECRET_IMPACT", filter, cursors[limit-1])
	}
	if err := tx.Commit(ctx); err != nil {
		return result, errs.ErrUnavailable
	}
	return result, nil
}

func (repository *Repository) rebindRuntimeSecret(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.RuntimeSecretRebindInput)
	if !ok || payload.Revision < 1 || input.Mutation.ExpectedVersion == nil || len(payload.Selections) < 1 || len(payload.Selections) > 32 {
		return commandOutcome{}, errs.ErrInvalid
	}
	secret, err := repository.lockRuntimeSecret(ctx, tx, current.organizationID, payload.SecretRef)
	if err != nil {
		return commandOutcome{}, err
	}
	if secret.version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	target, projectRef, err := repository.secretImpactTarget(ctx, tx, current, payload.SecretRef, payload.Revision)
	if err != nil {
		return commandOutcome{}, err
	}
	selections := append([]entity.RuntimeSecretRebindSelection(nil), payload.Selections...)
	sort.Slice(selections, func(i, j int) bool { return selections[i].EnvironmentRef < selections[j].EnvironmentRef })
	result := command.Result{}
	consumerCount := 0
	for i, selection := range selections {
		consumerCount += len(selection.Consumers)
		if selection.EnvironmentRef == "" || selection.SourceVersionRef == "" || selection.ExpectedEnvironmentVersion < 1 || consumerCount > 100 ||
			(i > 0 && selections[i-1].EnvironmentRef == selection.EnvironmentRef) {
			return commandOutcome{}, errs.ErrInvalid
		}
		_, selectedProject, err := repository.environmentImpactTarget(ctx, tx, current, selection.EnvironmentRef, selection.SourceVersionRef)
		if err != nil {
			return commandOutcome{}, err
		}
		if selectedProject != projectRef {
			return commandOutcome{}, errs.ErrNotFound
		}
		lookup := current
		lookup.role = "OWNER"
		environment, err := repository.getRuntimeEnvironmentTx(ctx, tx, lookup, selection.EnvironmentRef)
		if err != nil {
			return commandOutcome{}, err
		}
		if environment.Version != selection.ExpectedEnvironmentVersion {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		var revisionNumber int64
		if err := tx.QueryRow(ctx, querySecretRebindSourceVersion, current.organizationID, selection.EnvironmentRef, selection.SourceVersionRef).Scan(&revisionNumber); err != nil {
			return commandOutcome{}, errs.ErrNotFound
		}
		source, err := scanRuntimeEnvironmentVersion(tx.QueryRow(ctx, queryRuntimeConfigurationListEnvironmentVersions, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "environment_ref": selection.EnvironmentRef, "before_version": revisionNumber + 1, "platform_role": "OWNER", "actor_id": current.actorID, "page_size": 1}))
		if err != nil || source.Ref != selection.SourceVersionRef {
			return commandOutcome{}, errs.ErrUnavailable
		}
		bindings := make([]entity.RuntimeSecretBinding, 0, len(source.SecretDescriptors))
		changed := false
		for _, descriptor := range source.SecretDescriptors {
			revision := descriptor.Revision
			if descriptor.SecretRef == payload.SecretRef && revision != target.TargetRevision {
				revision = target.TargetRevision
				changed = true
			}
			bindings = append(bindings, entity.RuntimeSecretBinding{Name: descriptor.Name, SecretRef: descriptor.SecretRef, Revision: revision})
		}
		if !changed {
			return commandOutcome{}, errs.ErrConflict
		}
		for _, consumer := range selection.Consumers {
			if consumer.VersionRef != selection.SourceVersionRef {
				return commandOutcome{}, errs.ErrVersionMismatch
			}
		}
		publication := input
		publication.Kind = command.PublishRuntimeEnvironment
		publication.Mutation.ExpectedVersion = &selection.ExpectedEnvironmentVersion
		publication.Payload = command.RuntimeEnvironmentInput{Ref: environment.Ref, ProjectRef: environment.ProjectRef, Name: environment.Name, Description: environment.Description,
			Values: source.Values, SecretBindings: bindings, ImageArtifactRef: source.Image.ArtifactRef, Tools: source.Tools, Policy: source.Policy}
		published, err := repository.changeRuntimeEnvironment(ctx, tx, current, publication)
		if err != nil {
			return commandOutcome{}, err
		}
		if published.result.RuntimeEnvironment == nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if err := repository.emitCommandOutcomePlatformEvent(ctx, tx, current, published); err != nil {
			return commandOutcome{}, err
		}
		created := *published.result.RuntimeEnvironment
		result.RuntimeEnvironments = append(result.RuntimeEnvironments, created)
		if len(selection.Consumers) > 0 {
			rebind := input
			rebind.Kind = command.RebindRuntimeEnvironment
			rebind.Mutation.ExpectedVersion = &created.Version
			rebind.Payload = command.RuntimeEnvironmentRebindInput{EnvironmentRef: environment.Ref, VersionRef: created.CurrentVersion.Ref, Consumers: selection.Consumers}
			bound, err := repository.rebindRuntimeEnvironment(ctx, tx, current, rebind)
			if err != nil {
				return commandOutcome{}, err
			}
			result.EnvironmentBindings = append(result.EnvironmentBindings, bound.result.EnvironmentBindings...)
		}
	}
	return commandOutcome{result: result, resourceKind: "SECRET", resourceRef: payload.SecretRef, projectID: secret.projectID, projectRef: projectRef, summary: "i18n:RUNTIME_SECRET_SELECTED_REBOUND"}, nil
}
