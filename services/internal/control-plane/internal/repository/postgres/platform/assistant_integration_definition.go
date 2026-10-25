package platform

import (
	"context"
	"reflect"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	revisionservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/revision"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// hydrateAssistantIntegrationDefinitionPublication accepts only organization-scoped
// configuration/revision refs, resolves a nonarchived UI-owned integration definition and locks a
// VALID revision with a canonical digest. It builds server-derived target, before/after, pins and
// selection without copying OpenAPI content into the plan or publishing anything.
// hydrateAssistantIntegrationDefinitionPublication принимает только ссылки конфигурации и ревизии
// уровня организации, разрешает неархивное определение интеграции под управлением UI и блокирует
// VALID ревизию с каноническим дайджестом. Собирает серверные цель, before/after, привязки версий
// и выбор, не копируя OpenAPI в план и ничего не публикуя.
func (repository *Repository) hydrateAssistantIntegrationDefinitionPublication(
	ctx context.Context, tx pgx.Tx, current scope, projectRef string, operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	if projectRef != "" || !onlyAssistantFields(operation.Parameters, "configurationRef", "revisionRef") ||
		!hasAssistantFields(operation.Parameters, "configurationRef", "revisionRef") {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	configurationRef := assistantString(operation.Parameters, "configurationRef")
	revisionRef := assistantString(operation.Parameters, "revisionRef")
	if !strings.HasPrefix(configurationRef, "mcfg_") || len(configurationRef) > 96 ||
		!strings.HasPrefix(revisionRef, "mrev_") || len(revisionRef) > 96 {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	set, err := repository.resolveManagedSet(ctx, tx, current,
		command.ManagedConfigurationInput{ConfigurationRef: configurationRef}, revisionservice.KindIntegrationDefinition, false)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	if set.ProjectRef != "" || set.ManagedBy != "UI" || set.Archived {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	revision, err := repository.lockManagedRevision(ctx, tx, current, set, revisionRef)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	if revision.State != "VALID" || !validAssistantDefinitionDigest(revision.Digest) {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	currentRevisionRef := ""
	if set.CurrentRevision != nil {
		currentRevisionRef = set.CurrentRevision.Ref
	}
	version := set.Version
	operation.Action = "UPDATE"
	operation.Target = entity.AssistantPlanTarget{Kind: "INTEGRATION_DEFINITION", Ref: set.Ref, Name: set.Name, Version: &version}
	operation.Parameters = map[string]any{
		"configurationRef": set.Ref, "revisionRef": revision.Ref, "revisionDigest": revision.Digest,
	}
	operation.Before = map[string]any{
		"currentRevisionRef": currentRevisionRef, "revisionState": "VALID", "revisionDigest": revision.Digest,
	}
	operation.After = map[string]any{
		"currentRevisionRef": revision.Ref, "revisionState": "PUBLISHED", "revisionDigest": revision.Digest,
	}
	operation.ExpectedVersion = &version
	operation.Selected = true
	return operation, nil
}

// validAssistantDefinitionDigest accepts exactly 64 lowercase hexadecimal characters. It checks
// digest syntax only, without hashing content or proving revision identity.
// validAssistantDefinitionDigest принимает ровно 64 строчных шестнадцатеричных символа. Проверяет
// только форму дайджеста, не вычисляя хеш содержимого и не доказывая идентичность ревизии.
func validAssistantDefinitionDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return false
			}
		}
	}
	return true
}

// assistantIntegrationDefinitionPublicationCommand requires the closed publication input set,
// positive matching expected version, bounded ref prefixes, digest syntax and exact target
// identity. It constructs a typed PublishIntegrationDefinition command; digest equality and
// current authority are checked by other plan/publication stages, not supplied by this conversion.
// assistantIntegrationDefinitionPublicationCommand требует закрытый набор входных полей
// публикации, положительную совпадающую ожидаемую версию, ограниченные ссылки с нужными
// префиксами, форму дайджеста и точную цель. Собирает типизированную PublishIntegrationDefinition;
// равенство дайджеста и текущие полномочия проверяются другими стадиями плана и публикации, а не
// этим преобразованием.
func assistantIntegrationDefinitionPublicationCommand(operation entity.AssistantPlanOperation) (command.Command, error) {
	if !onlyAssistantFields(operation.Input, "configurationRef", "revisionRef", "revisionDigest", "expectedVersion") ||
		!hasAssistantFields(operation.Input, "configurationRef", "revisionRef", "revisionDigest", "expectedVersion") {
		return command.Command{}, errs.ErrInvalid
	}
	version, valid := assistantInt64(operation.Input, "expectedVersion")
	configurationRef := assistantString(operation.Input, "configurationRef")
	revisionRef := assistantString(operation.Input, "revisionRef")
	if !valid || version < 1 || operation.ExpectedVersion == nil || *operation.ExpectedVersion != version ||
		!strings.HasPrefix(configurationRef, "mcfg_") || len(configurationRef) > 96 ||
		!strings.HasPrefix(revisionRef, "mrev_") || len(revisionRef) > 96 ||
		!validAssistantDefinitionDigest(assistantString(operation.Input, "revisionDigest")) ||
		operation.Target.Kind != "INTEGRATION_DEFINITION" || operation.Target.Ref != configurationRef {
		return command.Command{}, errs.ErrInvalid
	}
	return command.Command{
		Kind: command.PublishIntegrationDefinition,
		Payload: command.ManagedConfigurationInput{
			ConfigurationRef: configurationRef, RevisionRef: revisionRef, Kind: revisionservice.KindIntegrationDefinition,
		},
		Mutation: value.Mutation{ExpectedVersion: &version},
	}, nil
}

// rehydrateEditedAssistantIntegrationDefinitionPublication requires the original operation
// type/key, integration-definition target, expected-version presence and unchanged parameters. It
// returns the original operation with only Selected, Title and Summary replaced from the edit,
// preventing edited authority or revision fields from entering the canonical operation.
// rehydrateEditedAssistantIntegrationDefinitionPublication требует исходные тип и ключ операции,
// цель определения интеграции, наличие ожидаемой версии и неизменные параметры. Возвращает
// исходную операцию, заменив только Selected, Title и Summary из правки, не пропуская изменённые
// полномочия или ревизию в каноническую операцию.
func rehydrateEditedAssistantIntegrationDefinitionPublication(original, edited entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	if original.Type != "PUBLISH_INTEGRATION_DEFINITION" || original.Key != edited.Key ||
		original.Target.Kind != "INTEGRATION_DEFINITION" || original.ExpectedVersion == nil ||
		!reflect.DeepEqual(edited.Parameters, original.Parameters) {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}
	original.Selected = edited.Selected
	original.Title = edited.Title
	original.Summary = edited.Summary
	return original, nil
}

// assistantIntegrationDefinitionPublicationSnapshotMatches rehydrates the exact stored
// configuration/revision refs through the current authorized path, then compares target,
// parameters, before/after and expected version. Lookup/authorization errors propagate; it
// performs no publication or transaction commit.
// assistantIntegrationDefinitionPublicationSnapshotMatches заново разрешает точные сохранённые
// ссылки конфигурации и ревизии через текущий проверенный путь, затем сравнивает цель, параметры,
// before/after и ожидаемую версию. Ошибки поиска и доступа передаются выше; публикация и
// завершение транзакции не выполняются.
func (repository *Repository) assistantIntegrationDefinitionPublicationSnapshotMatches(
	ctx context.Context, tx pgx.Tx, current scope, operation entity.AssistantPlanOperation,
) (bool, error) {
	parameters := map[string]any{
		"configurationRef": assistantString(operation.Parameters, "configurationRef"),
		"revisionRef":      assistantString(operation.Parameters, "revisionRef"),
	}
	fresh, err := repository.hydrateAssistantIntegrationDefinitionPublication(ctx, tx, current, "",
		entity.AssistantPlanOperation{Type: operation.Type, Parameters: parameters})
	if err != nil {
		return false, err
	}
	return reflect.DeepEqual(fresh.Target, operation.Target) &&
		reflect.DeepEqual(fresh.Parameters, operation.Parameters) &&
		reflect.DeepEqual(fresh.Before, operation.Before) &&
		reflect.DeepEqual(fresh.After, operation.After) &&
		reflect.DeepEqual(fresh.ExpectedVersion, operation.ExpectedVersion), nil
}
