package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// prepareProjectAssistantConnection Names the closed preparation operation for creating a
// project-assistant integration connection; preparation does not execute the final
// organization-scoped effect.
// prepareProjectAssistantConnection Задаёт закрытую операцию подготовки соединения интеграции
// проектного ассистента; подготовка не выполняет итоговый организационный эффект.
const prepareProjectAssistantConnection = "PREPARE_PROJECT_ASSISTANT_INTEGRATION_CONNECTION"

// authorizeAssistantPreparedOperation Delegates ordinary operations to canonical command
// authorization and the special connection preparation to its owner check with effect=false.
// Rejects a mismatched typed payload; final effect authorization remains stricter and separate.
// authorizeAssistantPreparedOperation Передаёт обычные операции канонической авторизации команд,
// специальную подготовку соединения — проверке владельца с effect=false. Отклоняет иной тип
// payload; более строгая авторизация итогового эффекта остаётся отдельно.
func (repository *Repository) authorizeAssistantPreparedOperation(ctx context.Context, tx pgx.Tx, current scope, operation entity.AssistantPlanOperation, input command.Command) error {
	if operation.Type != prepareProjectAssistantConnection {
		return repository.authorizeCommand(ctx, tx, current, input)
	}
	payload, valid := input.Payload.(command.ProjectAssistantConnectionInput)
	if !valid {
		return errs.ErrInvalid
	}
	return repository.authorizeProjectAssistantConnection(ctx, tx, current, payload, false)
}

// queryProjectAssistantConnectionOwner Locks the active project/non-system assistant/profile
// lineage under exact tenant/ref/project authority, project.manage visibility and active
// organization OWNER/ADMINISTRATOR membership. Returns owner versions/name; the caller repeats
// organization.manage authorization.
// queryProjectAssistantConnectionOwner Блокирует активную цепочку проекта, несистемного ассистента
// и профиля по точным организации, ссылке и проектной границе, видимости project.manage и
// активному организационному OWNER/ADMINISTRATOR. Возвращает версии и имя; вызывающий повторяет
// organization.manage.
//
//go:embed sql/project_assistant_connection__owner.sql
var queryProjectAssistantConnectionOwner string

// queryProjectAssistantConnectionInsert Inserts a purpose binding for the exact newly created
// same-organization connection with caller-resolved project/profile/assistant versions and actor.
// It does not independently validate those owner pins; the caller already checked them and
// requires one inserted row.
// queryProjectAssistantConnectionInsert Вставляет назначение точного нового соединения той же
// организации с разрешёнными проектом, профилем, версиями ассистента и субъектом. Самостоятельно
// эти привязки не проверяет; вызывающий проверяет их заранее и требует одну вставленную строку.
//
//go:embed sql/project_assistant_connection__insert.sql
var queryProjectAssistantConnectionInsert string

// queryProjectAssistantConnectionOperations Combines owner-function connection and
// integration-grant operation hints for exact organization/actor/assistant/scope/project
// authority. Returned names explain available actions without granting command authority.
// queryProjectAssistantConnectionOperations Объединяет подсказки функций владельца для соединений
// и разрешений по точным организации, субъекту, ассистенту, области и проектной границе. Имена
// объясняют действия без выдачи полномочий команды.
//
//go:embed sql/project_assistant_connection__operations.sql
var queryProjectAssistantConnectionOperations string

// projectConnectionEditable Defines the four trusted preparation input fields copied back into
// fresh connection hydration requests. Protected owner/version/package pins are not selected from
// edited parameters by this list.
// projectConnectionEditable Задаёт четыре доверенных поля подготовки для повторного свежего чтения
// соединения. Защищённые привязки владельца, версий и пакета этим списком из редактируемых
// параметров не выбираются.
var projectConnectionEditable = []string{"projectAssistantRef", "definitionKey", "name", "publicConfiguration"}

// projectConnectionPins Defines immutable
// assistant/scope/organization/project/profile/agent/package pins that owner edits must preserve
// before fresh hydration. These names are not independent authorization evidence.
// projectConnectionPins Задаёт неизменяемые привязки ассистента, области, организации, проекта,
// профиля, агента и пакета, сохраняемые правками до свежего чтения. Имена полей не являются
// самостоятельным подтверждением полномочий.
var projectConnectionPins = []string{"assistantScope", "scopeKind", "organizationRef", "projectRef", "assistantProfileRef", "agentVersion", "profileVersion", "definitionVersion", "definitionDigest"}

// projectAssistantConnectionOwner Reads exact owner lineage/versions and requires
// organization.manage, hiding access denial as not-found. Returns server-derived PROJECT assistant
// and ORGANIZATION connection scope metadata; no connection is created.
// projectAssistantConnectionOwner Читает точные владение и версии, требует organization.manage,
// скрывая отказ как NotFound. Возвращает серверные PROJECT область ассистента и ORGANIZATION
// область соединения; соединение не создаёт.
func (repository *Repository) projectAssistantConnectionOwner(ctx context.Context, tx pgx.Tx, current scope, assistantRef string) (map[string]any, error) {
	var projectRef, profileRef, name string
	var profileVersion, agentVersion int64
	err := tx.QueryRow(ctx, queryProjectAssistantConnectionOwner, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID,
		"assistant_ref": assistantRef, "authority_project": current.authorityProjectID,
	}).Scan(&projectRef, &profileRef, &profileVersion, &agentVersion, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errs.ErrNotFound
	}
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	if err := repository.requireAccess(ctx, tx, current, "organization.manage", organizationTarget(current.organizationRef)); err != nil {
		return nil, errs.ErrNotFound
	}
	return map[string]any{"projectAssistantRef": assistantRef, "assistantScope": "PROJECT", "scopeKind": "ORGANIZATION", "organizationRef": current.organizationRef,
		"projectRef": projectRef, "assistantProfileRef": profileRef, "agentVersion": agentVersion, "profileVersion": profileVersion, "assistantName": name}, nil
}

// hydrateProjectAssistantConnection Validates the closed editable fields and bootstrap
// configuration against the executable definition, resolves current owner and enabled package
// pins, then assigns immutable before/after/target/agent-version metadata for a selected CREATE
// plan. It prepares a candidate without executing a connection effect.
// hydrateProjectAssistantConnection Проверяет закрытые редактируемые поля и bootstrap-конфигурацию
// по исполняемому пакету, разрешает текущего владельца и включённую ревизию пакета. Назначает
// неизменяемые before, after, target и версию агента выбранного CREATE плана без создания
// соединения.
func (repository *Repository) hydrateProjectAssistantConnection(ctx context.Context, tx pgx.Tx, current scope, operation entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	if !onlyAssistantFields(operation.Parameters, projectConnectionEditable...) || !hasAssistantFields(operation.Parameters, projectConnectionEditable...) {
		return operation, errs.ErrInvalid
	}
	owner, err := repository.projectAssistantConnectionOwner(ctx, tx, current, assistantString(operation.Parameters, "projectAssistantRef"))
	if err != nil {
		return operation, err
	}
	definitionKey := assistantString(operation.Parameters, "definitionKey")
	definition, exists := repository.integrationDefinitions[definitionKey]
	configuration, valid := assistantObjectValue(operation.Parameters, "publicConfiguration")
	values, validValues := integrationStringConfiguration(configuration)
	name := strings.TrimSpace(assistantString(operation.Parameters, "name"))
	if !exists || !valid || !validValues || definition.ValidateConnectionBootstrapConfiguration(values) != nil || name == "" || len(name) > 160 {
		return operation, errs.ErrInvalid
	}
	var version, digest string
	if err := tx.QueryRow(ctx, queryConfigurationChangeconnectionSelectIntegrationDefinitionsStableKeyEnabled, definitionKey).Scan(&version, &digest); errors.Is(err, pgx.ErrNoRows) {
		return operation, errs.ErrNotFound
	} else if err != nil {
		return operation, errs.ErrUnavailable
	}
	if version != definition.Metadata.Version || digest != definition.Digest {
		return operation, errs.ErrConflict
	}
	params := cloneAssistantFields(owner)
	delete(params, "assistantName")
	params["definitionKey"], params["name"], params["publicConfiguration"] = definitionKey, name, configuration
	params["definitionVersion"], params["definitionDigest"] = version, digest
	operation.Parameters, operation.Before, operation.After = params, cloneAssistantFields(owner), cloneAssistantFields(params)
	operation.Before["definitionVersion"], operation.Before["definitionDigest"] = version, digest
	agentVersion, _ := assistantInt64(owner, "agentVersion")
	operation.ExpectedVersion = &agentVersion
	operation.Target = entity.AssistantPlanTarget{Kind: "PROJECT_ASSISTANT", Ref: assistantString(params, "projectAssistantRef"), Name: assistantString(owner, "assistantName"), Version: &agentVersion}
	operation.Action, operation.Selected = "CREATE", true
	return operation, nil
}

// projectAssistantConnectionCommand Maps a closed PROJECT-assistant/ORGANIZATION-scope input to
// the specialized connection command, requiring positive matching agent/expected version, profile
// version and nonempty owner/package pins plus string-valued public configuration. Fresh
// owner/definition checks remain in execution.
// projectAssistantConnectionCommand Преобразует закрытый вход PROJECT ассистента с ORGANIZATION
// областью в специализированную команду соединения. Требует положительные совпавшие agent/expected
// version, версию профиля, непустые привязки владельца и пакета, строковую публичную конфигурацию.
// Свежие проверки владельца и пакета остаются при исполнении.
func projectAssistantConnectionCommand(operation entity.AssistantPlanOperation) (command.Command, error) {
	allowed := append(append([]string{}, projectConnectionEditable...), projectConnectionPins...)
	allowed = append(allowed, "expectedVersion")
	if !onlyAssistantFields(operation.Input, allowed...) || !hasAssistantFields(operation.Input, allowed...) ||
		assistantString(operation.Input, "scopeKind") != "ORGANIZATION" || assistantString(operation.Input, "assistantScope") != "PROJECT" {
		return command.Command{}, errs.ErrInvalid
	}
	version, validVersion := assistantInt64(operation.Input, "expectedVersion")
	agentVersion, validAgentVersion := assistantInt64(operation.Input, "agentVersion")
	profileVersion, validProfileVersion := assistantInt64(operation.Input, "profileVersion")
	configuration, validConfig := assistantObjectValue(operation.Input, "publicConfiguration")
	_, validConfiguration := integrationStringConfiguration(configuration)
	if !validVersion || version < 1 || !validAgentVersion || agentVersion != version || !validProfileVersion || profileVersion < 1 || !validConfig || !validConfiguration ||
		!validCapabilityKey(assistantString(operation.Input, "definitionKey")) || assistantString(operation.Input, "name") == "" || len(assistantString(operation.Input, "name")) > 160 {
		return command.Command{}, errs.ErrInvalid
	}
	for _, field := range []string{"projectAssistantRef", "organizationRef", "projectRef", "assistantProfileRef", "definitionVersion", "definitionDigest"} {
		if assistantString(operation.Input, field) == "" {
			return command.Command{}, errs.ErrInvalid
		}
	}
	return command.Command{Kind: command.CreateProjectAssistantIntegrationConnection, Payload: command.ProjectAssistantConnectionInput{
		AssistantRef: assistantString(operation.Input, "projectAssistantRef"), OrganizationRef: assistantString(operation.Input, "organizationRef"),
		ProjectRef: assistantString(operation.Input, "projectRef"), ProfileRef: assistantString(operation.Input, "assistantProfileRef"),
		AgentVersion: version, ProfileVersion: profileVersion,
		DefinitionVersion: assistantString(operation.Input, "definitionVersion"), DefinitionDigest: assistantString(operation.Input, "definitionDigest"),
		Connection: command.ConnectionInput{DefinitionKey: assistantString(operation.Input, "definitionKey"), Name: assistantString(operation.Input, "name"), PublicConfiguration: configuration},
	}}, nil
}

// mustAssistantInt64 Returns the existing assistant integer conversion or zero when invalid,
// deliberately ignoring its boolean. Callers comparing owner-derived versions must not treat this
// convenience accessor as input validation.
// mustAssistantInt64 Возвращает результат прежнего преобразования целого ассистента либо ноль при
// ошибке, намеренно игнорируя boolean. Удобный доступ к версиям владельца не заменяет проверку
// входа.
func mustAssistantInt64(fields map[string]any, field string) int64 {
	value, _ := assistantInt64(fields, field)
	return value
}

// projectAssistantConnectionSnapshotMatches Rehydrates only the trusted editable parameter subset
// against current owner/package state and compares before/after/target JSON projections. Read
// failures propagate; equality is a plan snapshot check, not permission to create a connection.
// projectAssistantConnectionSnapshotMatches Свежо разрешает только доверенный редактируемый набор
// параметров по текущим владельцу и пакету, сравнивает JSON before, after и target. Ошибка чтения
// возвращается; равенство проверяет снимок плана, а не разрешает создание соединения.
func (repository *Repository) projectAssistantConnectionSnapshotMatches(ctx context.Context, tx pgx.Tx, current scope, original entity.AssistantPlanOperation) (bool, error) {
	request := assistantHydrationRequest(original, projectConnectionEditable)
	fresh, err := repository.hydrateProjectAssistantConnection(ctx, tx, current, request)
	if err != nil {
		return false, err
	}
	return assistantJSONEqual(fresh.Before, original.Before) && assistantJSONEqual(fresh.After, original.After) && assistantJSONEqual(fresh.Target, original.Target), nil
}

// rehydrateEditedProjectAssistantConnection Rejects edits to protected pins/assistant identity,
// requires the original snapshot to remain current unless refreshing stale work, then rehydrates
// editable fields and rechecks stable owner lineage before preserving selection. Error
// classes/order and separate effect authorization remain explicit.
// rehydrateEditedProjectAssistantConnection Отклоняет смену защищённых привязок и ассистента,
// требует свежесть исходного снимка вне обновления устаревшего плана, заново разрешает
// редактируемые поля и сверяет стабильное владение, сохраняя выбор. Порядок ошибок и отдельная
// авторизация эффекта остаются явными.
func (repository *Repository) rehydrateEditedProjectAssistantConnection(ctx context.Context, tx pgx.Tx, current scope, original, edited entity.AssistantPlanOperation, refreshStale bool) (entity.AssistantPlanOperation, error) {
	allowed := append(append([]string{}, projectConnectionEditable...), projectConnectionPins...)
	if !onlyAssistantFields(edited.Parameters, allowed...) || !hasAssistantFields(edited.Parameters, allowed...) {
		return edited, errs.ErrInvalid
	}
	for _, field := range append(append([]string{}, projectConnectionPins...), "projectAssistantRef") {
		if !assistantJSONEqual(edited.Parameters[field], original.Parameters[field]) {
			return edited, errs.ErrForbidden
		}
	}
	if !refreshStale {
		matching, err := repository.projectAssistantConnectionSnapshotMatches(ctx, tx, current, original)
		if err != nil || !matching {
			return edited, errs.ErrConflict
		}
	}
	request := assistantHydrationRequest(edited, projectConnectionEditable)
	fresh, err := repository.hydrateProjectAssistantConnection(ctx, tx, current, request)
	if err != nil {
		return edited, err
	}
	for _, field := range []string{"projectAssistantRef", "assistantScope", "scopeKind", "organizationRef", "projectRef", "assistantProfileRef"} {
		if !assistantJSONEqual(fresh.Parameters[field], original.Parameters[field]) {
			return edited, errs.ErrForbidden
		}
	}
	fresh.Selected = edited.Selected
	return fresh, nil
}

// authorizeProjectAssistantConnection For effects rejects project-limited authority, resolves
// current owner and requires exact organization/project/profile identity. Preparation may retain a
// project boundary, but neither payload IDs nor version pins bypass current owner authorization.
// authorizeProjectAssistantConnection Для эффекта отклоняет проектно ограниченные полномочия,
// разрешает текущего владельца и требует точные организацию, проект и профиль. Подготовка может
// сохранять проектную границу; ID payload и версии не обходят текущую авторизацию владельца.
func (repository *Repository) authorizeProjectAssistantConnection(ctx context.Context, tx pgx.Tx, current scope, payload command.ProjectAssistantConnectionInput, effect bool) error {
	if effect && current.authorityProjectID != "" {
		return errs.ErrNotFound
	}
	owner, err := repository.projectAssistantConnectionOwner(ctx, tx, current, payload.AssistantRef)
	if err != nil {
		return err
	}
	if payload.OrganizationRef != current.organizationRef || payload.ProjectRef != assistantString(owner, "projectRef") || payload.ProfileRef != assistantString(owner, "assistantProfileRef") {
		return errs.ErrNotFound
	}
	return nil
}

// createProjectAssistantConnection Repeats effect authority and exact owner/package version
// checks, delegates creation to canonical changeConnection and attaches the project-assistant
// purpose row in the same transaction. Requires a connection result and one purpose row; outer
// execution owns audit/idempotency/commit.
// createProjectAssistantConnection Повторяет полномочия эффекта и точные версии владельца с
// пакетом, передаёт создание каноническому changeConnection и связывает назначение проектного
// ассистента в той же транзакции. Требует результат соединения и одну строку назначения; аудит,
// идемпотентность и commit принадлежат внешнему исполнению.
func (repository *Repository) createProjectAssistantConnection(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, valid := input.Payload.(command.ProjectAssistantConnectionInput)
	if !valid {
		return commandOutcome{}, errs.ErrInvalid
	}
	if err := repository.authorizeProjectAssistantConnection(ctx, tx, current, payload, true); err != nil {
		return commandOutcome{}, err
	}
	owner, err := repository.projectAssistantConnectionOwner(ctx, tx, current, payload.AssistantRef)
	if err != nil {
		return commandOutcome{}, err
	}
	if payload.AgentVersion != mustAssistantInt64(owner, "agentVersion") || payload.ProfileVersion != mustAssistantInt64(owner, "profileVersion") {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	definition, exists := repository.integrationDefinitions[payload.Connection.DefinitionKey]
	if !exists || payload.DefinitionVersion != definition.Metadata.Version || payload.DefinitionDigest != definition.Digest {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	canonical := input
	canonical.Kind, canonical.Payload = command.CreateConnection, payload.Connection
	result, err := repository.changeConnection(ctx, tx, current, canonical)
	if err != nil {
		return result, err
	}
	if result.result.Connection == nil {
		return result, errs.ErrUnavailable
	}
	inserted, err := tx.Exec(ctx, queryProjectAssistantConnectionInsert, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID, "connection_ref": result.resourceRef,
		"project_ref": payload.ProjectRef, "profile_ref": payload.ProfileRef, "assistant_ref": payload.AssistantRef,
		"profile_version": payload.ProfileVersion, "agent_version": payload.AgentVersion,
	})
	if err != nil || inserted.RowsAffected() != 1 {
		return commandOutcome{}, errs.ErrUnavailable
	}
	return result, nil
}

// assistantHydrationRequest copies an operation envelope and replaces only Parameters with a fresh
// map selected by the caller-owned closed editable-field list. Missing selected keys stay present
// with nil, nested values remain shared and all other envelope fields are unchanged. It performs
// no validation, hydration or authorization; callers retain their distinct owner/version checks.
// assistantHydrationRequest копирует оболочку операции и заменяет только Parameters новой картой
// по закрытому списку редактируемых полей вызывающего. Отсутствующие выбранные ключи сохраняются
// с nil, вложенные значения остаются общими, остальные поля оболочки не меняются. Проверку,
// разрешение состояния и авторизацию не выполняет; разные проверки владельца и версий у callers.
func assistantHydrationRequest(operation entity.AssistantPlanOperation, editable []string) entity.AssistantPlanOperation {
	parameters := operation.Parameters
	operation.Parameters = map[string]any{}
	for _, field := range editable {
		operation.Parameters[field] = parameters[field]
	}
	return operation
}
