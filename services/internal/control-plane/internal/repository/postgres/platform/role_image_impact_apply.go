package platform

import (
	"context"
	"sort"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) publishRoleImageEnvironment(ctx context.Context, tx pgx.Tx, s scope, input command.Command, row roleImageImpactRow, item entity.RoleImageImpactItem, expected int64, projectID string) (entity.RuntimeEnvironmentSet, error) {
	plan := row.public
	var empty entity.RuntimeEnvironmentSet
	if _, _, err := r.environmentImpactTarget(ctx, tx, s, item.EnvironmentRef, item.SourceVersionRef); err != nil {
		return empty, err
	}
	lookup := s
	lookup.role = "OWNER"
	environment, err := r.getRuntimeEnvironmentTx(ctx, tx, lookup, item.EnvironmentRef)
	if err != nil {
		return empty, err
	}
	if environment.Version != expected {
		return empty, errs.ErrVersionMismatch
	}
	var number int64
	if tx.QueryRow(ctx, querySecretRebindSourceVersion, s.organizationID, item.EnvironmentRef, item.SourceVersionRef).Scan(&number) != nil {
		return empty, errs.ErrNotFound
	}
	source, err := scanRuntimeEnvironmentVersion(tx.QueryRow(ctx, queryRuntimeConfigurationListEnvironmentVersions, pgx.StrictNamedArgs{
		"organization_id": s.organizationID, "environment_ref": item.EnvironmentRef, "before_version": number + 1, "platform_role": "OWNER", "actor_id": s.actorID, "page_size": 1,
	}))
	if err != nil {
		return empty, err
	}
	if source.Ref != item.SourceVersionRef || source.Digest != item.SourceVersionDigest || source.Image.RecipeRef != plan.RecipeRef || source.Image.ArtifactRef == plan.ArtifactRef {
		return empty, errs.ErrConflict
	}
	secrets := make([]entity.RuntimeSecretBinding, 0, len(source.SecretDescriptors))
	for _, secret := range source.SecretDescriptors {
		secrets = append(secrets, entity.RuntimeSecretBinding{Name: secret.Name, SecretRef: secret.SecretRef, Revision: secret.Revision})
	}
	publication := input
	publication.Kind = command.PublishRuntimeEnvironment
	publication.Mutation.ExpectedVersion = &expected
	publication.Payload = command.RuntimeEnvironmentInput{Ref: environment.Ref, ProjectRef: environment.ProjectRef, Name: environment.Name, Description: environment.Description,
		Values: source.Values, SecretBindings: secrets, ImageArtifactRef: plan.ArtifactRef, Tools: source.Tools, Policy: source.Policy}
	published, err := r.changeRuntimeEnvironment(ctx, tx, s, publication)
	if err != nil {
		return empty, err
	}
	if published.result.RuntimeEnvironment == nil || published.result.RuntimeEnvironment.CurrentVersion.Ref == source.Ref {
		return empty, errs.ErrUnavailable
	}
	if err = r.emitCommandOutcomePlatformEvent(ctx, tx, s, published); err != nil {
		return empty, err
	}
	bindingRef, err := newRef("mcbind")
	if err != nil {
		return empty, errs.ErrUnavailable
	}
	var kind, consumer, revision string
	var bindingVersion int64
	if tx.QueryRow(ctx, queryManagedConfigurationRebindConsumer, pgx.StrictNamedArgs{"binding_ref": bindingRef, "organization_id": s.organizationID, "project_id": projectID,
		"configuration_set_id": row.configurationID, "revision_id": row.revisionID, "configuration_kind": "ROLE_IMAGE", "consumer_kind": "RUNTIME_ENVIRONMENT", "consumer_ref": environment.Ref, "actor_id": s.actorID}).Scan(&bindingRef, &kind, &consumer, &revision, &bindingVersion) != nil {
		return empty, errs.ErrUnavailable
	}
	if kind != "RUNTIME_ENVIRONMENT" || consumer != environment.Ref || revision != plan.RevisionRef || bindingVersion < 1 {
		return empty, errs.ErrUnavailable
	}
	return *published.result.RuntimeEnvironment, nil
}

func (r *Repository) applyRoleImageImpact(ctx context.Context, tx pgx.Tx, s scope, set managedSet, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.ManagedConfigurationInput)
	if !ok || payload.PlanRef == "" || len(payload.Consumers) > 0 || len(payload.SelectedItemRefs) > maximumRoleImageImpactItems {
		return commandOutcome{}, errs.ErrInvalid
	}
	row, err := r.roleImageImpact(ctx, tx, s, payload.PlanRef)
	if err != nil {
		return commandOutcome{}, err
	}
	_, revision, err := r.roleImageImpactAccess(ctx, tx, s, row)
	if err != nil {
		return commandOutcome{}, err
	}
	if row.public.State != "PREPARED" || row.public.ConfigurationRef != set.Ref || row.public.ConfigurationVersion != set.Version || row.public.RevisionRef != payload.RevisionRef || row.public.Digest != payload.ImpactDigest {
		return commandOutcome{}, errs.ErrConflict
	}
	items, err := r.roleImageImpactItems(ctx, tx, row.id)
	if err != nil {
		return commandOutcome{}, err
	}
	if int64(len(items)) != row.public.Total {
		return commandOutcome{}, errs.ErrUnavailable
	}
	digest, digestErr := roleImageImpactDigest(row.public, s.actorID, items)
	if digestErr != nil || digest != row.public.Digest {
		return commandOutcome{}, errs.ErrUnavailable
	}
	indices := map[string]int{}
	for i, item := range items {
		indices[item.Ref] = i
	}
	selected := map[string]bool{}
	groups := map[string][]int{}
	for _, ref := range payload.SelectedItemRefs {
		i, found := indices[ref]
		if !found || selected[ref] {
			return commandOutcome{}, errs.ErrInvalid
		}
		selected[ref] = true
		key := items[i].EnvironmentRef + "/" + items[i].SourceVersionRef
		groups[key] = append(groups[key], i)
	}
	for i := range items {
		items[i].Outcome = "NOT_SELECTED"
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	versions := map[string]int64{}
	for _, key := range keys {
		group := groups[key]
		source := items[group[0]]
		expected := source.EnvironmentVersion
		if own, found := versions[source.EnvironmentRef]; found {
			expected = own
		}
		batch, err := tx.Begin(ctx)
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		created, applyErr := r.publishRoleImageEnvironment(ctx, batch, s, input, row, source, expected, set.projectID)
		if applyErr != nil {
			_ = batch.Rollback(ctx)
			outcome := secretDraftImpactError(applyErr)
			if outcome == "" {
				return commandOutcome{}, applyErr
			}
			for _, index := range group {
				items[index].Outcome = outcome
			}
			continue
		}
		applied := 0
		for _, index := range group {
			item := &items[index]
			if item.EnvironmentVersion != source.EnvironmentVersion {
				_ = batch.Rollback(ctx)
				return commandOutcome{}, errs.ErrUnavailable
			}
			if item.Consumer.AgentRef != "" {
				attempt, err := batch.Begin(ctx)
				if err != nil {
					_ = batch.Rollback(ctx)
					return commandOutcome{}, errs.ErrUnavailable
				}
				binding := input
				binding.Kind = command.RebindRuntimeEnvironment
				binding.Mutation.ExpectedVersion = &created.Version
				binding.Payload = command.RuntimeEnvironmentRebindInput{EnvironmentRef: created.Ref, VersionRef: created.CurrentVersion.Ref, Consumers: []entity.RuntimeEnvironmentConsumer{item.Consumer}}
				bound, bindErr := r.rebindRuntimeEnvironment(ctx, attempt, s, binding)
				if bindErr != nil {
					_ = attempt.Rollback(ctx)
					item.Outcome = secretDraftImpactError(bindErr)
					if item.Outcome == "" {
						_ = batch.Rollback(ctx)
						return commandOutcome{}, bindErr
					}
					continue
				}
				if len(bound.result.EnvironmentBindings) != 1 {
					_ = attempt.Rollback(ctx)
					_ = batch.Rollback(ctx)
					return commandOutcome{}, errs.ErrUnavailable
				}
				result := bound.result.EnvironmentBindings[0]
				if result.Ref != item.Consumer.BindingRef || result.Version <= item.Consumer.BindingVersion {
					_ = attempt.Rollback(ctx)
					_ = batch.Rollback(ctx)
					return commandOutcome{}, errs.ErrUnavailable
				}
				if attempt.Commit(ctx) != nil {
					_ = batch.Rollback(ctx)
					return commandOutcome{}, errs.ErrUnavailable
				}
				item.ResultBindingRef, item.ResultBindingVersion = result.Ref, result.Version
			}
			item.Outcome, item.ResultEnvironmentVersionRef = "APPLIED", created.CurrentVersion.Ref
			applied++
		}
		if applied == 0 {
			_ = batch.Rollback(ctx)
			continue
		}
		if batch.Commit(ctx) != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		versions[source.EnvironmentRef] = created.Version
	}
	for _, item := range items {
		var ref string
		if tx.QueryRow(ctx, queryRoleImageImpactOutcome, pgx.StrictNamedArgs{"plan_id": row.id, "item_ref": item.Ref, "outcome": item.Outcome,
			"environment_version_ref": item.ResultEnvironmentVersionRef, "binding_ref": item.ResultBindingRef, "binding_version": item.ResultBindingVersion}).Scan(&ref) != nil || ref != item.Ref {
			return commandOutcome{}, errs.ErrUnavailable
		}
	}
	if tx.QueryRow(ctx, queryRoleImageImpactFinish, pgx.StrictNamedArgs{"plan_id": row.id}).Scan(&row.public.Version) != nil {
		return commandOutcome{}, errs.ErrConflict
	}
	row.public.State = "APPLIED"
	if tx.QueryRow(ctx, queryManagedConfigurationTouchSet, pgx.StrictNamedArgs{"configuration_set_id": set.id, "expected_version": set.Version}).Scan(&set.Version, &set.UpdatedAt) != nil {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	outcome := managedOutcome(set, &revision.ManagedConfigurationRevision)
	outcome.result.RoleImageImpactPlan = &row.public
	return outcome, nil
}
