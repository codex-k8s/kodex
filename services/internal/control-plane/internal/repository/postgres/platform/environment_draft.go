package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

func scanEnvironmentDraft(row rowScanner) (entity.RuntimeEnvironmentDraft, error) {
	var draft entity.RuntimeEnvironmentDraft
	var specification, diagnostics []byte
	err := row.Scan(&draft.Ref, &draft.ProjectRef, &draft.EnvironmentRef, &draft.ExpectedEnvironmentVersion,
		&draft.State, &draft.Version, &specification, &draft.ValidationDigest, &diagnostics, &draft.PublishedEnvironmentRef,
		&draft.BaseVersionRef, &draft.BaseRevision, &draft.SavedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return draft, errs.ErrNotFound
	}
	if err != nil || json.Unmarshal(specification, &draft.Specification) != nil || json.Unmarshal(diagnostics, &draft.Diagnostics) != nil {
		return entity.RuntimeEnvironmentDraft{}, errs.ErrUnavailable
	}
	return draft, nil
}

func (repository *Repository) GetRuntimeEnvironmentDraft(ctx context.Context, principal value.Principal, ref string) (entity.RuntimeEnvironmentDraft, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.RuntimeEnvironmentDraft{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return entity.RuntimeEnvironmentDraft{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	draft, err := scanEnvironmentDraft(tx.QueryRow(ctx, queryEnvironmentDraftGet, current.organizationID, ref))
	if err != nil {
		return draft, err
	}
	target, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{ResourceKind: "PROJECT", ResourceRef: draft.ProjectRef})
	if err != nil {
		return entity.RuntimeEnvironmentDraft{}, err
	}
	if current.authorityProjectID != "" && current.authorityProjectID != target.projectID {
		return entity.RuntimeEnvironmentDraft{}, errs.ErrNotFound
	}
	if err := repository.requireAccess(ctx, tx, current, "project.manage", target); err != nil {
		return entity.RuntimeEnvironmentDraft{}, err
	}
	return draft, tx.Commit(ctx)
}

func (repository *Repository) changeRuntimeEnvironmentDraft(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.RuntimeEnvironmentDraftInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	var draft entity.RuntimeEnvironmentDraft
	var err error
	if input.Kind == command.CreateRuntimeEnvironmentDraft {
		var baseVersionRef string
		var baseRevision int64
		if len(asJSON(payload.Specification)) > 256<<10 || payload.ProjectRef == "" {
			return commandOutcome{}, errs.ErrInvalid
		}
		if payload.EnvironmentRef != "" {
			environment, err := repository.getRuntimeEnvironmentTx(ctx, tx, current, payload.EnvironmentRef)
			if err != nil {
				return commandOutcome{}, err
			}
			if environment.ProjectRef != payload.ProjectRef {
				return commandOutcome{}, errs.ErrNotFound
			}
			if environment.Version != payload.ExpectedEnvironmentVersion {
				return commandOutcome{}, errs.ErrVersionMismatch
			}
			baseVersionRef, baseRevision = environment.CurrentVersion.Ref, environment.CurrentVersion.Revision
			if baseVersionRef == "" || baseRevision <= 0 {
				return commandOutcome{}, errs.ErrUnavailable
			}
		} else if payload.ExpectedEnvironmentVersion != 0 {
			return commandOutcome{}, errs.ErrInvalid
		}
		ref, err := newRef("renvd")
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		draft = entity.RuntimeEnvironmentDraft{Ref: ref, Version: 1, ProjectRef: payload.ProjectRef, EnvironmentRef: payload.EnvironmentRef,
			BaseVersionRef: baseVersionRef, BaseRevision: baseRevision,
			ExpectedEnvironmentVersion: payload.ExpectedEnvironmentVersion, Specification: payload.Specification, State: "DRAFT", Diagnostics: []string{}}
		err = tx.QueryRow(ctx, queryEnvironmentDraftInsert, pgx.StrictNamedArgs{"ref": ref, "organization_id": current.organizationID,
			"project_id": mustProjectID(ctx, tx, current.organizationID, payload.ProjectRef), "environment_ref": draft.EnvironmentRef,
			"environment_version": draft.ExpectedEnvironmentVersion, "specification": asJSON(draft.Specification), "actor_id": current.actorID,
			"base_version_ref": draft.BaseVersionRef}).Scan(&draft.SavedAt)
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	} else {
		draft, err = scanEnvironmentDraft(tx.QueryRow(ctx, queryEnvironmentDraftLock, current.organizationID, payload.DraftRef))
		if err != nil {
			return commandOutcome{}, err
		}
		if input.Mutation.ExpectedVersion == nil || *input.Mutation.ExpectedVersion != draft.Version {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if draft.State == "PUBLISHED" || draft.State == "DISCARDED" {
			return commandOutcome{}, errs.ErrConflict
		}
		var environment *entity.RuntimeEnvironmentSet
		switch input.Kind {
		case command.SaveRuntimeEnvironmentDraft:
			if len(asJSON(payload.Specification)) > 256<<10 {
				return commandOutcome{}, errs.ErrInvalid
			}
			draft.Specification, draft.State, draft.ValidationDigest, draft.Diagnostics = payload.Specification, "DRAFT", "", []string{}
		case command.DiscardRuntimeEnvironmentDraft:
			draft.State = "DISCARDED"
		case command.ValidateRuntimeEnvironmentDraft, command.PublishRuntimeEnvironmentDraft:
			digest, err := repository.validateEnvironmentDraft(ctx, tx, current, draft)
			if input.Kind == command.ValidateRuntimeEnvironmentDraft {
				if errors.Is(err, errs.ErrUnavailable) {
					return commandOutcome{}, err
				}
				draft.State, draft.ValidationDigest, draft.Diagnostics = "VALID", digest, []string{}
				if err != nil {
					draft.State, draft.ValidationDigest, draft.Diagnostics = "INVALID", "", []string{"ENVIRONMENT_VALIDATION_FAILED"}
				}
			} else {
				if err != nil {
					return commandOutcome{}, err
				}
				if draft.State != "VALID" || digest != draft.ValidationDigest {
					return commandOutcome{}, errs.ErrConflict
				}
				publication := input
				publication.Kind = command.CreateRuntimeEnvironment
				if draft.EnvironmentRef != "" {
					publication.Kind = command.PublishRuntimeEnvironment
					publication.Mutation.ExpectedVersion = &draft.ExpectedEnvironmentVersion
				}
				publication.Payload = environmentDraftPayload(draft)
				outcome, err := repository.changeRuntimeEnvironment(ctx, tx, current, publication)
				if err != nil {
					return commandOutcome{}, err
				}
				environment = outcome.result.RuntimeEnvironment
				if environment == nil || environment.CurrentVersion.Digest != draft.ValidationDigest {
					return commandOutcome{}, errs.ErrConflict
				}
				draft.State, draft.PublishedEnvironmentRef = "PUBLISHED", environment.Ref
			}
		default:
			return commandOutcome{}, errs.ErrInvalid
		}
		err = tx.QueryRow(ctx, queryEnvironmentDraftUpdate, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "ref": draft.Ref, "version": draft.Version, "specification": asJSON(draft.Specification),
			"state": draft.State, "validation_digest": draft.ValidationDigest, "diagnostics": asJSON(draft.Diagnostics), "published_ref": draft.PublishedEnvironmentRef,
			"save_content": input.Kind == command.SaveRuntimeEnvironmentDraft}).Scan(&draft.SavedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		draft.Version++
		return environmentDraftOutcome(current, draft, environment, mustProjectID(ctx, tx, current.organizationID, draft.ProjectRef)), nil
	}
	return environmentDraftOutcome(current, draft, nil, mustProjectID(ctx, tx, current.organizationID, draft.ProjectRef)), nil
}

func environmentDraftPayload(draft entity.RuntimeEnvironmentDraft) command.RuntimeEnvironmentInput {
	spec := draft.Specification
	return command.RuntimeEnvironmentInput{Ref: draft.EnvironmentRef, ProjectRef: draft.ProjectRef, Name: spec.Name, Description: spec.Description,
		Values: spec.Values, SecretBindings: spec.SecretBindings, Tools: spec.Tools, Policy: spec.Policy, ImageArtifactRef: spec.ImageArtifactRef}
}

func (repository *Repository) validateEnvironmentDraft(ctx context.Context, tx pgx.Tx, current scope, draft entity.RuntimeEnvironmentDraft) (string, error) {
	spec := draft.Specification
	if strings.TrimSpace(spec.Name) == "" || len(spec.Name) > 120 || len(spec.Description) > 1000 {
		return "", errs.ErrInvalid
	}
	projectID := mustProjectID(ctx, tx, current.organizationID, draft.ProjectRef)
	policy, err := repository.admitRuntimeEnvironmentPolicy(ctx, tx, current, draft.ProjectRef, draft.EnvironmentRef, spec.Policy)
	if err != nil {
		return "", err
	}
	_, _, values, secrets, err := repository.resolveEnvironmentPayload(ctx, tx, current.organizationID, projectID, spec.Values, spec.SecretBindings)
	if err != nil {
		return "", err
	}
	_, image, tools, _, err := repository.resolveRuntimeEnvironmentImage(ctx, tx, current.organizationID, projectID, spec.ImageArtifactRef, spec.Tools)
	if err != nil {
		return "", err
	}
	_, digest, err := runtimeEnvironmentConfigurationDigests(values, secrets, image, tools, policy)
	return digest, err
}

func environmentDraftOutcome(_ scope, draft entity.RuntimeEnvironmentDraft, environment *entity.RuntimeEnvironmentSet, projectID string) commandOutcome {
	outcome := commandOutcome{result: command.Result{RuntimeEnvironmentDraft: &draft, RuntimeEnvironment: environment},
		projectID: projectID, projectRef: draft.ProjectRef, resourceKind: "RUNTIME_ENVIRONMENT_DRAFT", resourceRef: draft.Ref,
		summary: "i18n:RUNTIME_ENVIRONMENT_DRAFT_CHANGED"}
	if environment != nil {
		outcome.resourceKind, outcome.resourceRef, outcome.platformEvent = "RUNTIME_ENVIRONMENT", environment.Ref, "AGENT_CHANGED"
		outcome.platformAggregateVersion = environment.Version
	}
	return outcome
}
