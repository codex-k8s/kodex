package platform

import (
	"context"
	_ "embed"
	"errors"
	"sort"
	"strconv"
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

// querySecretImpactTarget embeds the active secret metadata lookup for a requested revision or the
// current revision when zero is supplied. It returns owner scope and project identifiers within
// one organization, never the secret value.
// querySecretImpactTarget встраивает поиск метаданных активного секрета для запрошенной ревизии
// либо текущей при нулевом номере. Возвращает область владельца и идентификаторы проекта внутри
// одной организации, но не значение секрета.
//
//go:embed sql/secret_impact_target.sql
var querySecretImpactTarget string

// querySecretImpactConsumers embeds actor- and authority-project-filtered environment revision
// consumers that reference another secret revision. It returns a cursor page plus total, including
// eligible unbound current revisions, without reading secret bytes or granting update permission.
// querySecretImpactConsumers встраивает поиск потребителей ревизий окружения со ссылкой на другую
// ревизию секрета, с фильтрацией по субъекту и проекту полномочий. Возвращает страницу с курсором
// и общее количество, включая допустимые текущие ревизии без привязок, без чтения содержимого
// секрета и выдачи разрешения на изменение.
//
//go:embed sql/secret_impact_consumers.sql
var querySecretImpactConsumers string

// querySecretRebindSourceVersion embeds the exact organization/environment/revision lookup of a
// published environment version number. The caller separately checks owner access and the loaded
// history revision identity.
// querySecretRebindSourceVersion встраивает поиск номера опубликованной версии окружения по точным
// организации, окружению и ревизии. Вызывающий код отдельно проверяет доступ владельца и
// идентичность ревизии, загруженной из истории.
//
//go:embed sql/secret_rebind_source_version.sql
var querySecretRebindSourceVersion string

// queryRuntimeSecretRevisionRetained embeds the retention predicate for an active secret revision:
// the current secret revision or one referenced by a current environment, explicit agent binding
// or active run runtime revision remains retained. This predicate reads metadata only.
// queryRuntimeSecretRevisionRetained встраивает условие удержания активной ревизии секрета:
// сохраняется текущая ревизия либо ревизия, используемая текущим окружением, явной привязкой
// агента или runtime-ревизией активного запуска. Условие читает только метаданные.
//
//go:embed sql/runtime_secret_revision_retained.sql
var queryRuntimeSecretRevisionRetained string

// queryRuntimeSecretRetireRevision embeds the update that marks a selected secret ID and revision
// RETIRED. The caller must supply a trusted secret ID and enforce retention, authorization and
// transaction boundaries; this statement does not erase secret bytes.
// queryRuntimeSecretRetireRevision встраивает перевод выбранных идентификатора секрета и ревизии в
// RETIRED. Вызывающий код обязан получить доверенный идентификатор и обеспечить удержание,
// авторизацию и границы транзакции; запрос не стирает содержимое секрета.
//
//go:embed sql/runtime_secret_retire_revision.sql
var queryRuntimeSecretRetireRevision string

// secretImpactTarget rejects negative revisions, loads active secret revision metadata within the
// organization and enforces authority-project and owner-scope checks plus secret.rotate access. It
// returns impact metadata and the private project ID without retrieving secret values.
// secretImpactTarget отклоняет отрицательный номер ревизии, загружает метаданные активной ревизии
// секрета внутри организации и проверяет проект полномочий, область владельца и доступ
// secret.rotate. Возвращает метаданные влияния и внутренний идентификатор проекта без извлечения
// значений секрета.
func (repository *Repository) secretImpactTarget(ctx context.Context, tx pgx.Tx, current scope, ref string, revision int64) (entity.RuntimeSecretImpact, string, error) {
	var result entity.RuntimeSecretImpact
	if revision < 0 {
		return result, "", errs.ErrInvalid
	}
	var projectRef, projectID, scopeKind, organizationRef string
	err := tx.QueryRow(ctx, querySecretImpactTarget, current.organizationID, ref, revision).Scan(&result.SecretRef, &result.SecretVersion, &result.TargetRevision, &projectRef, &projectID, &scopeKind, &organizationRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, "", errs.ErrNotFound
	}
	if err != nil {
		return result, "", errs.ErrUnavailable
	}
	if current.authorityProjectID != "" && current.authorityProjectID != projectID {
		return result, "", errs.ErrForbidden
	}
	if organizationRef != current.organizationRef || !validRuntimeOwnerSnapshot(scopeKind, organizationRef, projectRef) {
		return result, "", errs.ErrNotFound
	}
	if scopeKind == "ORGANIZATION" {
		if err := repository.requireOrganizationRuntimeResourceAccess(ctx, tx, current, "secret.rotate"); err != nil {
			return result, "", err
		}
		return result, projectRef, nil
	}
	if err := repository.requireAccess(ctx, tx, current, "secret.rotate", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "SECRET", ResourceRef: ref}); err != nil {
		return result, "", err
	}
	return result, projectRef, nil
}

// GetRuntimeSecretImpact validates bounded UTF-8 search before opening a five-second
// repeatable-read, read-only transaction, then authorizes the secret target and reads a
// cursor-bound consumer page. It uses an extra row for the next cursor and commits; partial
// metadata on failure is not a successful page or permission to mutate consumers.
// GetRuntimeSecretImpact проверяет ограничения UTF-8 поиска до открытия read-only транзакции
// RepeatableRead с бюджетом пять секунд, затем проверяет доступ к секрету и читает связанную с
// курсором страницу потребителей. Использует дополнительную строку для следующего курсора и
// завершает транзакцию; частичные метаданные при ошибке не означают успешную страницу или
// разрешение менять потребителей.
func (repository *Repository) GetRuntimeSecretImpact(ctx context.Context, principal value.Principal, ref string, revision int64, search string, page query.Page) (entity.RuntimeSecretImpact, error) {
	search = strings.TrimSpace(search)
	if !utf8.ValidString(search) || utf8.RuneCountInString(search) > 200 || strings.ContainsRune(search, 0) {
		return entity.RuntimeSecretImpact{}, errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, tx, err := repository.beginScopedSnapshotRead(ctx, principal)
	if err != nil {
		return entity.RuntimeSecretImpact{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, _, err := repository.secretImpactTarget(ctx, tx, current, ref, revision)
	if err != nil {
		return result, err
	}
	filter := query.Filter{ResourceRef: ref, Query: search, Category: strconv.FormatInt(result.TargetRevision, 10), Page: page}
	cursor, err := decodeCatalogCursor(current, "SECRET_IMPACT", filter)
	if err != nil {
		return result, err
	}
	limit := boundedPage(page)
	rows, err := tx.Query(ctx, querySecretImpactConsumers, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "query": search,
		"authority_project": current.authorityProjectID, "secret_ref": ref, "target_revision": result.TargetRevision, "evaluated_at": time.Now().UTC(), "cursor_ref": cursor, "page_size": limit + 1})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	var cursors []string
	for rows.Next() {
		var item entity.RuntimeSecretImpactConsumer
		var key string
		if err := rows.Scan(&key, &item.EnvironmentRef, &item.EnvironmentVersion, &item.EnvironmentVersionRef, &item.SecretRevisions,
			&item.Consumer.AgentRef, &item.Consumer.AgentVersion, &item.Consumer.BindingRef, &item.Consumer.BindingVersion, &item.ProjectRef, &item.ScopeKind, &item.OrganizationRef, &result.Total); err != nil {
			rows.Close()
			return result, errs.ErrUnavailable
		}
		if key != "" {
			item.Consumer.VersionRef = item.EnvironmentVersionRef
			item.Consumer.ScopeKind, item.Consumer.OrganizationRef, item.Consumer.ProjectRef = item.ScopeKind, item.OrganizationRef, item.ProjectRef
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

// rebindRuntimeSecret authorizes a typed secret rebind with a positive target revision, expected
// secret version and up to 32 unique environment selections and 100 consumers. It validates owner
// snapshots and exact source revisions, publishes new environment descriptors and optionally
// rebinds selected agents in the same caller transaction. The internal OWNER lookup is used only
// after selection authorization; no secret bytes are materialized and no separate commit occurs.
// rebindRuntimeSecret проверяет полномочия типизированной перепривязки секрета с положительной
// целевой ревизией, ожидаемой версией секрета и максимум 32 уникальными окружениями и 100
// потребителями. Проверяет снимки владельца и точные исходные ревизии, публикует новые дескрипторы
// окружений и при необходимости перепривязывает выбранных агентов в той же транзакции. Внутреннее
// чтение с OWNER применяется только после авторизации выбора; содержимое секретов не извлекается,
// отдельного завершения транзакции нет.
func (repository *Repository) rebindRuntimeSecret(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.RuntimeSecretRebindInput)
	if !ok || payload.Revision < 1 || input.Mutation.ExpectedVersion == nil || len(payload.Selections) < 1 || len(payload.Selections) > 32 {
		return commandOutcome{}, errs.ErrInvalid
	}
	secret, err := repository.authorizeRuntimeSecretSelections(ctx, tx, current, payload.SecretRef, payload.Revision, payload.Selections)
	if err != nil {
		return commandOutcome{}, err
	}
	target, projectRef, err := repository.secretImpactTarget(ctx, tx, current, payload.SecretRef, payload.Revision)
	if err != nil {
		return commandOutcome{}, err
	}
	if secret.version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
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
		if err := matchRuntimeOwnerSnapshot(environment.ScopeKind, environment.OrganizationRef, environment.ProjectRef,
			selection.ScopeKind, selection.OrganizationRef, selection.ProjectRef); err != nil {
			return commandOutcome{}, err
		}
		if environment.ScopeKind != secret.scopeKind || environment.OrganizationRef != secret.organizationRef || environment.ProjectRef != secret.projectRef {
			return commandOutcome{}, errs.ErrNotFound
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
