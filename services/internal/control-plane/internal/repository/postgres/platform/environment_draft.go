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

// scanEnvironmentDraft scans draft metadata and decodes specification and diagnostics JSON. A
// missing row returns NotFound; storage or JSON failures return Unavailable. The scan does not
// authorize access or validate the draft specification.
// scanEnvironmentDraft читает метаданные черновика и декодирует JSON спецификации и диагностики.
// Отсутствие строки даёт NotFound; ошибки хранилища или JSON — Unavailable. Чтение не подтверждает
// доступ и не проверяет допустимость спецификации.
func scanEnvironmentDraft(row rowScanner) (entity.RuntimeEnvironmentDraft, error) {
	var draft entity.RuntimeEnvironmentDraft
	var specification, diagnostics []byte
	err := row.Scan(&draft.Ref, &draft.ProjectRef, &draft.EnvironmentRef, &draft.ExpectedEnvironmentVersion,
		&draft.State, &draft.Version, &specification, &draft.ValidationDigest, &diagnostics, &draft.PublishedEnvironmentRef,
		&draft.BaseVersionRef, &draft.BaseRevision, &draft.SavedAt, &draft.ScopeKind, &draft.OrganizationRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return draft, errs.ErrNotFound
	}
	if err != nil || json.Unmarshal(specification, &draft.Specification) != nil || json.Unmarshal(diagnostics, &draft.Diagnostics) != nil {
		return entity.RuntimeEnvironmentDraft{}, errs.ErrUnavailable
	}
	return draft, nil
}

// GetRuntimeEnvironmentDraft reads the tenant-scoped draft and checks environment-owner access in
// a repeatable-read, read-only transaction using the supplied context. It returns the draft with
// the raw commit error and retains deferred rollback; it adds no local deadline.
// GetRuntimeEnvironmentDraft читает черновик организации и проверяет доступ владельца окружения в
// read-only транзакции RepeatableRead с переданным контекстом. Возвращает черновик с исходной
// ошибкой Commit и сохраняет отложенный rollback; собственного deadline не добавляет.
func (repository *Repository) GetRuntimeEnvironmentDraft(ctx context.Context, principal value.Principal, ref string) (entity.RuntimeEnvironmentDraft, error) {
	current, tx, err := repository.beginScopedSnapshotRead(ctx, principal)
	if err != nil {
		return entity.RuntimeEnvironmentDraft{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	draft, err := scanEnvironmentDraft(tx.QueryRow(ctx, queryEnvironmentDraftGet, current.organizationID, ref))
	if err != nil {
		return draft, err
	}
	if err := repository.requireRuntimeEnvironmentOwnerAccess(ctx, tx, current, draft.ScopeKind, draft.ProjectRef); err != nil {
		return entity.RuntimeEnvironmentDraft{}, err
	}
	return draft, tx.Commit(ctx)
}

// changeRuntimeEnvironmentDraft handles typed draft creation, saving, validation, discard and
// publication in the supplied transaction. It checks owner scope, bounded specification size,
// existing-draft locks and expected versions; terminal drafts cannot change. Validation stores a
// digest and diagnostics, and publication rechecks the validated digest before publishing and
// optionally rebinding selected consumers. It returns an outcome without committing, so all writes
// share the outer command transaction.
// changeRuntimeEnvironmentDraft обрабатывает типизированные создание, сохранение, проверку,
// отбрасывание и публикацию черновика в переданной транзакции. Проверяет область владельца, размер
// спецификации, блокировку существующего черновика и ожидаемые версии; конечные состояния
// неизменяемы. Проверка сохраняет дайджест и диагностику, публикация повторно сверяет проверенный
// дайджест перед публикацией и возможной перепривязкой выбранных потребителей. Возвращает
// результат без завершения транзакции, сохраняя все записи в общей транзакции команды.
func (repository *Repository) changeRuntimeEnvironmentDraft(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.RuntimeEnvironmentDraftInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	var draft entity.RuntimeEnvironmentDraft
	var err error
	if input.Kind == command.CreateRuntimeEnvironmentDraft || input.Kind == command.CreateOrganizationRuntimeEnvironmentDraft {
		scopeKind := "PROJECT"
		if input.Kind == command.CreateOrganizationRuntimeEnvironmentDraft {
			scopeKind = "ORGANIZATION"
		}
		var baseVersionRef string
		var baseRevision int64
		if len(asJSON(payload.Specification)) > 256<<10 || scopeKind == "PROJECT" && payload.ProjectRef == "" || scopeKind == "ORGANIZATION" && payload.ProjectRef != "" {
			return commandOutcome{}, errs.ErrInvalid
		}
		if err := repository.requireRuntimeEnvironmentOwnerAccess(ctx, tx, current, scopeKind, payload.ProjectRef); err != nil {
			return commandOutcome{}, err
		}
		if payload.EnvironmentRef != "" {
			environment, err := repository.getRuntimeEnvironmentTx(ctx, tx, current, payload.EnvironmentRef)
			if err != nil {
				return commandOutcome{}, err
			}
			if environment.ProjectRef != payload.ProjectRef || environment.ScopeKind != scopeKind || environment.OrganizationRef != current.organizationRef {
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
		draft = entity.RuntimeEnvironmentDraft{ScopeKind: scopeKind, OrganizationRef: current.organizationRef, Ref: ref, Version: 1, ProjectRef: payload.ProjectRef, EnvironmentRef: payload.EnvironmentRef,
			BaseVersionRef: baseVersionRef, BaseRevision: baseRevision,
			ExpectedEnvironmentVersion: payload.ExpectedEnvironmentVersion, Specification: payload.Specification, State: "DRAFT", Diagnostics: []string{}}
		err = tx.QueryRow(ctx, queryEnvironmentDraftInsert, pgx.StrictNamedArgs{"ref": ref, "organization_id": current.organizationID,
			"scope_kind": scopeKind, "project_id": mustProjectID(ctx, tx, current.organizationID, payload.ProjectRef), "environment_ref": draft.EnvironmentRef,
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
		if err := repository.requireRuntimeEnvironmentOwnerAccess(ctx, tx, current, draft.ScopeKind, draft.ProjectRef); err != nil {
			return commandOutcome{}, err
		}
		if input.Mutation.ExpectedVersion == nil || *input.Mutation.ExpectedVersion != draft.Version {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if draft.State == "PUBLISHED" || draft.State == "DISCARDED" {
			return commandOutcome{}, errs.ErrConflict
		}
		var environment *entity.RuntimeEnvironmentSet
		var impactRow revisionImpactRow
		var impactItems []entity.RevisionImpactItem
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
				if errors.Is(err, errs.ErrUnavailable) || errors.Is(err, errs.ErrFreshAuthenticationRequired) {
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
				impactRow, impactItems, err = repository.environmentDraftImpactForPublish(ctx, tx, current, draft, payload)
				if err != nil {
					return commandOutcome{}, err
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
		result := environmentDraftOutcome(current, draft, environment, mustProjectID(ctx, tx, current.organizationID, draft.ProjectRef))
		if environment != nil {
			plan, applyErr := repository.applyEnvironmentDraftImpact(ctx, tx, current, input, impactRow, impactItems, *environment)
			if applyErr != nil {
				return commandOutcome{}, applyErr
			}
			result.result.RevisionImpactPlan = &plan
		}
		return result, nil
	}
	return environmentDraftOutcome(current, draft, nil, mustProjectID(ctx, tx, current.organizationID, draft.ProjectRef)), nil
}

// environmentDraftPayload projects draft specification fields and its environment reference into a
// runtime-environment command input. Slices and maps remain shared; the projection neither
// authorizes nor publishes anything.
// environmentDraftPayload переносит поля спецификации черновика и ссылку на окружение во вход
// команды окружения. Срезы и отображения остаются общими; преобразование само не проверяет
// полномочия и ничего не публикует.
func environmentDraftPayload(draft entity.RuntimeEnvironmentDraft) command.RuntimeEnvironmentInput {
	spec := draft.Specification
	return command.RuntimeEnvironmentInput{ScopeKind: draft.ScopeKind, Ref: draft.EnvironmentRef, ProjectRef: draft.ProjectRef, Name: spec.Name, Description: spec.Description,
		Values: spec.Values, SecretBindings: spec.SecretBindings, Tools: spec.Tools, Policy: spec.Policy, ImageArtifactRef: spec.ImageArtifactRef}
}

// validateEnvironmentDraft checks owner access and bounded name/description, admits policy,
// resolves scoped secret descriptors and image/tool metadata, then computes the configuration
// digest. It returns the first failure and does not materialize secret values, publish a version
// or commit.
// validateEnvironmentDraft проверяет доступ владельца и ограничения имени и описания, допустимость
// политики, дескрипторы секретов и метаданные образа и инструментов в нужной области, затем
// вычисляет дайджест конфигурации. Возвращает первую ошибку; не извлекает значения секретов, не
// публикует версию и не завершает транзакцию.
func (repository *Repository) validateEnvironmentDraft(ctx context.Context, tx pgx.Tx, current scope, draft entity.RuntimeEnvironmentDraft) (string, error) {
	spec := draft.Specification
	if err := repository.requireRuntimeEnvironmentOwnerAccess(ctx, tx, current, draft.ScopeKind, draft.ProjectRef); err != nil {
		return "", err
	}
	if strings.TrimSpace(spec.Name) == "" || len(spec.Name) > 120 || len(spec.Description) > 1000 {
		return "", errs.ErrInvalid
	}
	projectID := mustProjectID(ctx, tx, current.organizationID, draft.ProjectRef)
	policy, err := repository.admitRuntimeEnvironmentPolicy(ctx, tx, current, draft.ProjectRef, draft.EnvironmentRef, spec.Policy)
	if err != nil {
		return "", err
	}
	_, _, values, secrets, err := repository.resolveScopedEnvironmentPayload(ctx, tx, current.organizationID, projectID, draft.ScopeKind, spec.Values, spec.SecretBindings)
	if err != nil {
		return "", err
	}
	_, image, tools, _, err := repository.resolveScopedRuntimeEnvironmentImage(ctx, tx, current.organizationID, projectID, draft.ScopeKind, spec.ImageArtifactRef, spec.Tools)
	if err != nil {
		return "", err
	}
	_, digest, err := runtimeEnvironmentConfigurationDigests(values, secrets, image, tools, policy)
	return digest, err
}

// environmentDraftOutcome builds the draft result and summary without database I/O. When
// publication returned an environment, it changes the resource reference and aggregate version and
// includes the existing AGENT_CHANGED event; event persistence and commit belong to the outer
// command processor.
// environmentDraftOutcome собирает результат черновика и краткое описание без обращения к БД. Если
// публикация вернула окружение, меняет ссылку ресурса и версию агрегата и включает существующее
// событие AGENT_CHANGED; сохранение события и завершение транзакции выполняет внешний обработчик
// команды.
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
