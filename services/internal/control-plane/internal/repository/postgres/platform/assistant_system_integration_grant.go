package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// querySystemAssistantIntegrationGrantSnapshot Reads existing exact
// organization/connection/agent/capability grant state or empty/default fields through a left
// join. It supplies a candidate snapshot, not owner authorization or a new grant.
// querySystemAssistantIntegrationGrantSnapshot Читает состояние точного grant организации,
// соединения, агента и возможности либо пустые поля через LEFT JOIN. Даёт снимок кандидата, а не
// авторизацию владельца или новый grant.
//
//go:embed sql/system_assistant_integration_grants__snapshot.sql
var querySystemAssistantIntegrationGrantSnapshot string

// querySystemAssistantIntegrationGrantPlanAuthority Reads the actor-owned plan operations used to
// recheck project and system grant authority before execution. The caller strictly bounds the
// decoded operation count and resolves each current owner; stored pins alone do not authorize
// effects.
// querySystemAssistantIntegrationGrantPlanAuthority Читает операции принадлежащего субъекту плана
// для повторной проверки проектных и системных grant перед исполнением. Вызывающий ограничивает
// число операций и разрешает каждого текущего владельца; сохранённые привязки сами не разрешают
// эффект.
//
//go:embed sql/system_assistant_integration_grants__plan_authority.sql
var querySystemAssistantIntegrationGrantPlanAuthority string

// authorizeSystemAssistantIntegrationGrantPlan For plan inputs reads exact actor-owned operations,
// validates the bound and project-grant authority, then requires each system operation to match
// organization scope and current assistant owner. Other payload kinds are skipped; no grants are
// changed.
// authorizeSystemAssistantIntegrationGrantPlan Для входа плана читает точные операции субъекта,
// проверяет предел и полномочия проектных grant, затем требует организационную область и текущего
// владельца каждой системной операции. Иные payload пропускаются; grant не меняются.
func (repository *Repository) authorizeSystemAssistantIntegrationGrantPlan(ctx context.Context, tx pgx.Tx, current scope, input command.Command) error {
	var ref string
	switch payload := input.Payload.(type) {
	case command.AssistantPlanInput:
		ref = payload.PlanRef
	case command.AssistantPlanDraftInput:
		ref = payload.PlanRef
	default:
		return nil
	}
	var raw []byte
	err := tx.QueryRow(ctx, querySystemAssistantIntegrationGrantPlanAuthority, current.organizationID, current.actorID, ref).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	var operations []entity.AssistantPlanOperation
	if json.Unmarshal(raw, &operations) != nil || len(operations) > maximumAssistantPlanOperations {
		return errs.ErrUnavailable
	}
	if err := repository.authorizeProjectAssistantIntegrationGrantPlan(ctx, tx, current, operations); err != nil {
		return err
	}
	for _, operation := range operations {
		if operation.Type != "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT" {
			continue
		}
		if assistantString(operation.Parameters, "scopeKind") != "ORGANIZATION" || assistantString(operation.Parameters, "organizationRef") != current.organizationRef {
			return errs.ErrNotFound
		}
		owner, err := repository.systemAssistantIntegrationGrantOwner(ctx, tx, current, assistantString(operation.Parameters, "connectionRef"))
		if err != nil {
			return err
		}
		if owner.ref != assistantString(operation.Parameters, "systemAssistantRef") {
			return errs.ErrNotFound
		}
	}
	return nil
}

// assistantSystemGrantEditable Defines five trusted connection/capability/enable/approval fields
// selected for fresh SYSTEM grant hydration. Project-assistant identifiers and protected owner
// pins are not copied from editable parameters.
// assistantSystemGrantEditable Задаёт пять доверенных полей соединения, возможности, включения и
// одобрения для свежего SYSTEM grant. Идентификаторы проектного ассистента и защищённые привязки
// владельца из редактируемых параметров не копируются.
var assistantSystemGrantEditable = []string{"connectionRef", "capabilityKey", "enabled", "approvalPolicy", "approvalScopePaths"}

// assistantSystemGrantOwner Defines protected organization/system-assistant/package/grant/approval
// metadata preserved by owner edits and freshly revalidated through the canonical owner path.
// assistantSystemGrantOwner Задаёт защищённые метаданные организации, системного ассистента,
// пакета, grant и одобрения, сохраняемые правками и заново проверяемые каноническим путём
// владельца.
var assistantSystemGrantOwner = []string{"scopeKind", "organizationRef", "systemAssistantRef", "definitionVersion", "definitionDigest", "grantRef", "grantVersion", "defaultApprovalPolicy", "allowedApprovalPolicies"}

// hydrateSystemAssistantIntegrationGrant Validates the closed editable grant input and approval
// paths, resolves canonical system-assistant owner/current connection/package capability, reads
// existing grant and rejects no-op changes. Builds immutable owner/before/after/target/version
// metadata without executing an effect.
// hydrateSystemAssistantIntegrationGrant Проверяет закрытый редактируемый вход grant и approval
// paths, разрешает канонического системного ассистента, текущее соединение и возможность пакета,
// читает прежний grant и отклоняет отсутствие изменения. Строит неизменяемые owner, before, after,
// target и version без исполнения эффекта.
func (repository *Repository) hydrateSystemAssistantIntegrationGrant(ctx context.Context, tx pgx.Tx, current scope, operation entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	if !onlyAssistantFields(operation.Parameters, assistantSystemGrantEditable...) {
		return operation, errs.ErrInvalid
	}
	payload, err := assistantGrantHydrationPayload(operation.Parameters)
	if err != nil {
		return operation, err
	}
	enabled, paths := payload.Enabled, payload.ApprovalScopePaths
	_, target, err := repository.systemAssistantIntegrationGrantInput(ctx, tx, current, payload)
	if err != nil {
		return operation, err
	}
	connection, err := repository.systemAssistantGrantHeader(ctx, tx, current, payload.ConnectionRef)
	if err != nil {
		return operation, err
	}
	definition, err := repository.integrationPackage(ctx, tx, current.organizationID, connection.ref, connection.definitionKey, connection.definitionVersion, connection.definitionDigest)
	if err != nil {
		return operation, err
	}
	capability, err := assistantGrantHydrationCapability(definition, payload)
	if err != nil {
		return operation, err
	}
	snapshot, err := readAssistantGrantPlanSnapshot(ctx, tx, current.organizationID, connection.ref, target.ref, payload.CapabilityKey)
	if err != nil {
		return operation, err
	}
	grantRef, grantVersion, policy, existingEnabled, existingPaths := snapshot.ref, snapshot.version, snapshot.policy, snapshot.enabled, snapshot.paths
	if grantRef == "" {
		policy = capability.ApprovalPolicy
	}
	if enabled == existingEnabled && policy == payload.ApprovalPolicy && assistantJSONEqual(sortedApprovalScopePaths(existingPaths), sortedApprovalScopePaths(paths)) {
		return operation, errs.ErrConflict
	}
	params := map[string]any{"connectionRef": connection.ref, "capabilityKey": payload.CapabilityKey, "enabled": enabled, "approvalPolicy": payload.ApprovalPolicy, "approvalScopePaths": append([]string{}, paths...), "scopeKind": "ORGANIZATION", "organizationRef": current.organizationRef, "systemAssistantRef": target.ref, "definitionVersion": connection.definitionVersion, "definitionDigest": connection.definitionDigest, "grantRef": grantRef, "grantVersion": grantVersion, "defaultApprovalPolicy": capability.ApprovalPolicy, "allowedApprovalPolicies": append([]string{}, capability.AllowedApprovalPolicies...)}
	return assistantGrantHydratedOperation(operation, params, existingEnabled, policy, existingPaths, connection.ref, connection.name, connection.version), nil
}

// systemAssistantIntegrationGrantCommand Maps a closed organization/system-assistant plan input
// with positive expected version and valid enable/approval fields to the specialized grant
// command. Current owner and package authority are checked separately during execution.
// systemAssistantIntegrationGrantCommand Преобразует закрытый организационный вход системного
// ассистента с положительной ожидаемой версией и допустимыми enable/approval полями в
// специализированную команду grant. Текущие полномочия владельца и пакета проверяются отдельно при
// исполнении.
func systemAssistantIntegrationGrantCommand(operation entity.AssistantPlanOperation) (command.Command, error) {
	allowed := append(append([]string{}, assistantSystemGrantEditable...), assistantSystemGrantOwner...)
	allowed = append(allowed, "expectedVersion")
	if !onlyAssistantFields(operation.Input, allowed...) || !hasAssistantFields(operation.Input, append(append([]string{}, assistantSystemGrantEditable...), assistantSystemGrantOwner...)...) {
		return command.Command{}, errs.ErrInvalid
	}
	version, versionOK := assistantInt64(operation.Input, "expectedVersion")
	enabled, enabledOK := assistantBoolValue(operation.Input, "enabled")
	paths, pathsOK := assistantStringsValue(operation.Input, "approvalScopePaths")
	if !versionOK || version < 1 || !enabledOK || !pathsOK || !validIntegrationApprovalPolicy(assistantString(operation.Input, "approvalPolicy")) || assistantString(operation.Input, "scopeKind") != "ORGANIZATION" || assistantString(operation.Input, "organizationRef") == "" || assistantString(operation.Input, "systemAssistantRef") == "" {
		return command.Command{}, errs.ErrInvalid
	}
	return command.Command{Kind: command.ChangeSystemAssistantIntegrationGrant, Mutation: value.Mutation{ExpectedVersion: &version}, Payload: command.SystemAssistantIntegrationGrantInput{ConnectionRef: assistantString(operation.Input, "connectionRef"), CapabilityKey: assistantString(operation.Input, "capabilityKey"), ApprovalPolicy: assistantString(operation.Input, "approvalPolicy"), Enabled: enabled, ApprovalScopePaths: paths}}, nil
}

// systemAssistantIntegrationGrantSnapshotMatches Rehydrates only trusted editable fields against
// current system owner/grant state and compares before/after/expected-version/target JSON. Errors
// propagate; matching is a freshness check, not execution authority.
// systemAssistantIntegrationGrantSnapshotMatches Свежо разрешает только доверенные редактируемые
// поля по текущим системному владельцу и grant, сравнивает JSON before, after, expected-version и
// target. Ошибка возвращается; совпадение проверяет свежесть, а не выдаёт полномочия.
func (repository *Repository) systemAssistantIntegrationGrantSnapshotMatches(ctx context.Context, tx pgx.Tx, current scope, original entity.AssistantPlanOperation) (bool, error) {
	request := assistantHydrationRequest(original, assistantSystemGrantEditable)
	fresh, err := repository.hydrateSystemAssistantIntegrationGrant(ctx, tx, current, request)
	if err != nil {
		return false, err
	}
	return assistantGrantPlanSnapshotsEqual(fresh, original), nil
}

// rehydrateEditedSystemAssistantGrant Preserves owner pins and exact connection/capability
// identity, checks original freshness outside stale refresh and rehydrates editable fields before
// rechecking stable organization/system-assistant scope and selection. PROJECT rules remain in
// their distinct path.
// rehydrateEditedSystemAssistantGrant Сохраняет привязки владельца и точные соединение с
// возможностью, проверяет свежесть вне обновления устаревшего плана, разрешает редактируемые поля
// и заново сверяет организацию, системного ассистента и выбор. PROJECT правила остаются в
// отдельном пути.
func (repository *Repository) rehydrateEditedSystemAssistantGrant(ctx context.Context, tx pgx.Tx, current scope, original, edited entity.AssistantPlanOperation, stale bool) (entity.AssistantPlanOperation, error) {
	if !onlyAssistantFields(edited.Parameters, append(append([]string{}, assistantSystemGrantEditable...), assistantSystemGrantOwner...)...) {
		return edited, errs.ErrInvalid
	}
	if !assistantGrantFieldsEqual(original.Parameters, edited.Parameters, assistantSystemGrantOwner) {
		return edited, errs.ErrForbidden
	}
	if !assistantGrantFieldsEqual(original.Parameters, edited.Parameters, []string{"connectionRef", "capabilityKey"}) {
		return edited, errs.ErrForbidden
	}
	if !stale {
		same, err := repository.systemAssistantIntegrationGrantSnapshotMatches(ctx, tx, current, original)
		if err != nil || !same {
			return edited, errs.ErrConflict
		}
	}
	request := assistantHydrationRequest(edited, assistantSystemGrantEditable)
	fresh, err := repository.hydrateSystemAssistantIntegrationGrant(ctx, tx, current, request)
	if err != nil {
		return edited, err
	}
	if !assistantGrantFieldsEqual(fresh.Parameters, original.Parameters, []string{"scopeKind", "organizationRef", "systemAssistantRef"}) {
		return edited, errs.ErrForbidden
	}
	fresh.Selected = edited.Selected
	return fresh, nil
}
