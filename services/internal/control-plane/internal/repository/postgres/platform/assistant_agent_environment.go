package platform

import (
	"context"
	"errors"
	"reflect"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// readAssistantAgentBindingSnapshot resolves the AGENT assistant context and requires the binding
// operation before reading agent and runtime configuration snapshots. It rejects mismatched agent
// versions or binding identity and returns only the binding fields needed by a plan; no grant or
// commit is produced.
// readAssistantAgentBindingSnapshot разрешает контекст ассистента AGENT и требует допустимости
// операции привязки перед чтением снимков агента и runtime-конфигурации. Отклоняет несовпадение
// версии агента или идентичности привязки, возвращает только поля привязки для плана; не выдаёт
// разрешение и не завершает транзакцию.
func (repository *Repository) readAssistantAgentBindingSnapshot(ctx context.Context, tx pgx.Tx, actorScope scope,
	projectRef, agentRef string,
) (map[string]any, int64, error) {
	projection, err := repository.resolveAssistantContext(ctx, tx, actorScope,
		entity.AssistantContextDescriptor{EntityKind: "AGENT", EntityRef: agentRef}, projectRef)
	if err != nil {
		return nil, 0, err
	}
	if !contains(projection.AllowedOperations, "BIND_AGENT_RUNTIME_ENVIRONMENT") {
		return nil, 0, errs.ErrForbidden
	}
	var name, purpose, roleDescription, avatarURL string
	var version int64
	if err := tx.QueryRow(ctx, queryConfigurationHydrateassistantoperationSelectAgent,
		actorScope.organizationID, projectRef, agentRef,
	).Scan(&name, &purpose, &roleDescription, &avatarURL, &version); errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, errs.ErrNotFound
	} else if err != nil {
		return nil, 0, errs.ErrUnavailable
	}
	view, err := repository.getRuntimeConfigurationViewTx(ctx, tx, actorScope, agentRef)
	if err != nil {
		return nil, 0, err
	}
	if view.AgentVersion != version || view.EnvironmentBinding.AgentRef != agentRef {
		return nil, 0, errs.ErrConflict
	}
	return map[string]any{
		"agentRef": agentRef, "projectRef": projectRef, "agentName": name,
		"bindingRef":      view.EnvironmentBinding.Ref,
		"environmentRef":  view.EnvironmentBinding.EnvironmentRef,
		"environmentName": view.Environment.Name,
		"versionRef":      view.EnvironmentBinding.VersionRef,
	}, version, nil
}

// readAssistantBindingTarget resolves the ENVIRONMENT assistant context and loads an ACTIVE, ready
// environment in the exact project with a nonempty current revision and digest. It returns target
// metadata, leaving binding mutation and transaction completion to callers.
// readAssistantBindingTarget разрешает контекст ассистента ENVIRONMENT и загружает ACTIVE
// окружение нужного проекта, готовое и имеющее непустые текущую ревизию и дайджест. Возвращает
// метаданные цели; изменение привязки и завершение транзакции остаются у вызывающего кода.
func (repository *Repository) readAssistantBindingTarget(ctx context.Context, tx pgx.Tx, actorScope scope,
	projectRef, environmentRef string,
) (map[string]any, error) {
	if _, err := repository.resolveAssistantContext(ctx, tx, actorScope,
		entity.AssistantContextDescriptor{EntityKind: "ENVIRONMENT", EntityRef: environmentRef}, projectRef); err != nil {
		return nil, err
	}
	environment, err := repository.getRuntimeEnvironmentTx(ctx, tx, actorScope, environmentRef)
	if err != nil {
		return nil, err
	}
	if environment.ProjectRef != projectRef || environment.State != "ACTIVE" || !environment.Ready ||
		environment.CurrentVersion.Ref == "" || environment.CurrentVersion.Digest == "" {
		return nil, errs.ErrConflict
	}
	return map[string]any{
		"environmentRef": environment.Ref, "environmentName": environment.Name,
		"versionRef":    environment.CurrentVersion.Ref,
		"versionDigest": environment.CurrentVersion.Digest,
	}, nil
}

// hydrateAssistantAgentEnvironmentBinding accepts only agentRef/environmentRef parameters in a
// nonempty project, reads both authorized snapshots and builds a canonical binding operation.
// Empty refs or unexpected fields are Invalid; snapshot errors propagate without writes.
// hydrateAssistantAgentEnvironmentBinding принимает только параметры agentRef/environmentRef для
// непустого проекта, читает оба проверенных снимка и собирает каноническую операцию привязки.
// Пустые ссылки или лишние поля дают Invalid; ошибки снимков передаются выше без записи.
func (repository *Repository) hydrateAssistantAgentEnvironmentBinding(ctx context.Context, tx pgx.Tx, actorScope scope,
	projectRef string, operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	if projectRef == "" || !onlyAssistantFields(operation.Parameters, "agentRef", "environmentRef") {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	agentRef, environmentRef := assistantString(operation.Parameters, "agentRef"), assistantString(operation.Parameters, "environmentRef")
	if agentRef == "" || environmentRef == "" {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	before, version, err := repository.readAssistantAgentBindingSnapshot(ctx, tx, actorScope, projectRef, agentRef)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	after, err := repository.readAssistantBindingTarget(ctx, tx, actorScope, projectRef, environmentRef)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	return hydrateAssistantBindingFields(before, after, version, operation)
}

// hydrateAssistantBindingFields rejects an unchanged environment/revision pair, then replaces
// target, before/after, parameters and expected version with server-derived values. It selects the
// operation and clears Input; before/after are copied through cloneAssistantFields, while
// unrelated operation metadata is preserved.
// hydrateAssistantBindingFields отклоняет неизменную пару окружения и ревизии, затем заменяет
// цель, before/after, параметры и ожидаемую версию серверными значениями. Выбирает операцию и
// очищает Input; before/after копируются через cloneAssistantFields, остальные метаданные операции
// сохраняются.
func hydrateAssistantBindingFields(before, after map[string]any, version int64,
	operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	if assistantString(before, "environmentRef") == assistantString(after, "environmentRef") &&
		assistantString(before, "versionRef") == assistantString(after, "versionRef") {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	operation.Action = "UPDATE"
	operation.Target = entity.AssistantPlanTarget{Kind: "AGENT", Ref: assistantString(before, "agentRef"),
		Name: assistantString(before, "agentName"), Version: &version}
	operation.Before = cloneAssistantFields(before)
	operation.After = cloneAssistantFields(after)
	operation.Parameters = map[string]any{
		"agentRef": before["agentRef"], "environmentRef": after["environmentRef"], "versionRef": after["versionRef"],
	}
	operation.ExpectedVersion = &version
	operation.Selected = true
	operation.Input = nil
	return operation, nil
}

// rehydrateEditedAssistantBinding requires the original binding operation identity and a closed
// edited parameter set, then rereads the original agent version and before snapshot. It rejects
// drift, resolves the edited environment and rebuilds canonical fields while preserving the edited
// Selected flag. It does not execute the binding.
// rehydrateEditedAssistantBinding требует исходную идентичность операции привязки и закрытый набор
// изменённых параметров, затем заново читает версию агента и снимок before. Отклоняет изменения
// исходного состояния, разрешает выбранное окружение и пересобирает канонические поля, сохраняя
// изменённый флаг Selected. Не выполняет привязку.
func (repository *Repository) rehydrateEditedAssistantBinding(ctx context.Context, tx pgx.Tx, actorScope scope,
	projectRef string, original, edited entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	if original.Type != "BIND_AGENT_RUNTIME_ENVIRONMENT" || original.Key != edited.Key ||
		original.Target.Kind != "AGENT" || original.Target.Ref == "" ||
		original.ExpectedVersion == nil || *original.ExpectedVersion < 1 || edited.Parameters == nil ||
		!onlyAssistantFields(edited.Parameters, "agentRef", "environmentRef", "versionRef") ||
		assistantString(edited.Parameters, "agentRef") != original.Target.Ref {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}
	before, version, err := repository.readAssistantAgentBindingSnapshot(ctx, tx, actorScope, projectRef, original.Target.Ref)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	if version != *original.ExpectedVersion || !reflect.DeepEqual(before, original.Before) {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	after, err := repository.readAssistantBindingTarget(ctx, tx, actorScope, projectRef,
		assistantString(edited.Parameters, "environmentRef"))
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	selected := edited.Selected
	hydrated, err := hydrateAssistantBindingFields(before, after, version, edited)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	hydrated.Selected = selected
	return hydrated, nil
}

// assistantAgentBindingSnapshotMatches delegates project-configuration operations to their
// specialized matcher; otherwise rereads authorized agent and environment snapshots and compares
// both version pins, name, before/after and binding parameters. Read failures propagate rather
// than becoming a false match; it performs no writes.
// assistantAgentBindingSnapshotMatches передаёт операции конфигурации проекта специализированной
// проверке; иначе заново читает проверенные снимки агента и окружения и сравнивает обе версии,
// имя, before/after и параметры привязки. Ошибки чтения передаются выше, а не превращаются в
// отрицательное совпадение; запись не выполняется.
func (repository *Repository) assistantAgentBindingSnapshotMatches(ctx context.Context, tx pgx.Tx, actorScope scope,
	projectRef string, operation entity.AssistantPlanOperation,
) (bool, error) {
	if assistantProjectConfigurationOperation(operation) {
		return repository.projectAssistantConfigurationSnapshotMatches(ctx, tx, actorScope, operation)
	}
	before, version, err := repository.readAssistantAgentBindingSnapshot(ctx, tx, actorScope, projectRef, operation.Target.Ref)
	if err != nil {
		return false, err
	}
	after, err := repository.readAssistantBindingTarget(ctx, tx, actorScope, projectRef,
		assistantString(operation.Parameters, "environmentRef"))
	if err != nil {
		return false, err
	}
	return operation.ExpectedVersion != nil && *operation.ExpectedVersion == version &&
		operation.Target.Version != nil && *operation.Target.Version == version &&
		operation.Target.Name == assistantString(before, "agentName") &&
		reflect.DeepEqual(operation.Before, before) && reflect.DeepEqual(operation.After, after) &&
		assistantString(operation.Parameters, "agentRef") == operation.Target.Ref &&
		assistantString(operation.Parameters, "versionRef") == assistantString(after, "versionRef"), nil
}

// assistantAgentEnvironmentBindingCommand requires exactly the binding input fields and checks
// their correspondence to target and after values, plus a positive expected version and nonempty
// environment/revision refs. It constructs a typed BindAgentRuntimeEnvironment command; this
// conversion is not fresh authorization.
// assistantAgentEnvironmentBindingCommand требует точный набор входных полей привязки, сверяет их
// с целью и after, проверяет положительную ожидаемую версию и непустые ссылки окружения и ревизии.
// Собирает типизированную команду BindAgentRuntimeEnvironment; преобразование не заменяет свежую
// авторизацию.
func assistantAgentEnvironmentBindingCommand(operation entity.AssistantPlanOperation) (command.Command, error) {
	input := operation.Input
	if !onlyAssistantFields(input, "agentRef", "environmentRef", "versionRef", "expectedVersion") ||
		!hasAssistantFields(input, "agentRef", "environmentRef", "versionRef", "expectedVersion") ||
		assistantString(input, "agentRef") != operation.Target.Ref ||
		assistantString(input, "environmentRef") != assistantString(operation.After, "environmentRef") ||
		assistantString(input, "versionRef") != assistantString(operation.After, "versionRef") {
		return command.Command{}, errs.ErrInvalid
	}
	version, ok := assistantInt64(input, "expectedVersion")
	if !ok || version < 1 || assistantString(input, "environmentRef") == "" || assistantString(input, "versionRef") == "" {
		return command.Command{}, errs.ErrInvalid
	}
	return command.Command{Kind: command.BindAgentRuntimeEnvironment,
		Mutation: value.Mutation{ExpectedVersion: &version},
		Payload: command.RuntimeEnvironmentBindingInput{AgentRef: assistantString(input, "agentRef"),
			EnvironmentRef: assistantString(input, "environmentRef"), VersionRef: assistantString(input, "versionRef")}}, nil
}

// lockAssistantBindingEnvironment takes a shared lock on an ACTIVE environment in the exact
// organization and active project. A missing row is Conflict; backend failure or an unexpected
// returned ref is Unavailable. Permission and revision checks remain separate.
// lockAssistantBindingEnvironment получает разделяемую блокировку ACTIVE окружения точной
// организации и активного проекта. Отсутствие строки даёт Conflict; ошибка хранилища либо
// неожиданная возвращённая ссылка — Unavailable. Проверки полномочий и ревизии выполняются
// отдельно.
func (repository *Repository) lockAssistantBindingEnvironment(ctx context.Context, tx pgx.Tx, actorScope scope,
	projectRef, environmentRef string,
) error {
	var ref string
	err := tx.QueryRow(ctx, queryConfigurationLockAssistantBindingEnvironment,
		actorScope.organizationID, projectRef, environmentRef).Scan(&ref)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrConflict
	}
	if err != nil || ref != environmentRef {
		return errs.ErrUnavailable
	}
	return nil
}
