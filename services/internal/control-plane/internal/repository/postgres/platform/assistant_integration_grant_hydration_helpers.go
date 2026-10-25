package platform

import (
	"context"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// assistantGrantPlanSnapshot holds the exact existing grant tuple read after scope-specific owner
// resolution. Empty ref denotes absence; the caller chooses the package default policy and checks
// no-op changes. It contains no credential or execution authority.
// assistantGrantPlanSnapshot хранит точный прежний grant после отдельного разрешения владельца.
// Пустой ref означает отсутствие; вызывающий выбирает default policy пакета и проверяет отсутствие
// изменения. Снимок не содержит credential или полномочий исполнения.
type assistantGrantPlanSnapshot struct {
	ref, policy string
	version     int64
	enabled     bool
	paths       []string
}

// assistantGrantHydrationPayload parses enabled, optional nonnil approval paths and the closed
// capability key, enforcing path count and disabled-path constraints. The caller must first reject
// unknown fields and later resolve its exact SYSTEM or PROJECT owner; no grant is issued.
// assistantGrantHydrationPayload разбирает enabled, необязательные ненулевые approval paths и
// закрытый capability key, ограничивает число paths и запрещает их для выключенного grant.
// Вызывающий заранее отклоняет неизвестные поля и отдельно разрешает точного владельца SYSTEM
// или PROJECT; функция не выдаёт grant.
func assistantGrantHydrationPayload(parameters map[string]any) (command.SystemAssistantIntegrationGrantInput, error) {
	enabled, enabledOK := assistantBoolValue(parameters, "enabled")
	paths := []string{}
	if _, supplied := parameters["approvalScopePaths"]; supplied {
		var ok bool
		paths, ok = assistantStringsValue(parameters, "approvalScopePaths")
		if !ok {
			return command.SystemAssistantIntegrationGrantInput{}, errs.ErrInvalid
		}
	}
	payload := command.SystemAssistantIntegrationGrantInput{ConnectionRef: assistantString(parameters, "connectionRef"), CapabilityKey: assistantString(parameters, "capabilityKey"), ApprovalPolicy: assistantString(parameters, "approvalPolicy"), Enabled: enabled, ApprovalScopePaths: paths}
	if !enabledOK || !validCapabilityKey(payload.CapabilityKey) || len(paths) > 16 || !enabled && len(paths) != 0 {
		return command.SystemAssistantIntegrationGrantInput{}, errs.ErrInvalid
	}
	return payload, nil
}

// assistantGrantHydrationCapability resolves the selected package capability and retains the
// exact callable/approval/scope-path checks. Owner/package admission belongs to the caller;
// rejection returns Invalid before reading existing grant state.
// assistantGrantHydrationCapability разрешает выбранную возможность пакета и сохраняет точные
// проверки callable, approval и scope paths. Допуск владельца и пакета принадлежит вызывающему;
// отказ возвращает Invalid до чтения прежнего grant.
func assistantGrantHydrationCapability(definition integrationpackage.Package, payload command.SystemAssistantIntegrationGrantInput) (integrationpackage.Capability, error) {
	enabled, paths := payload.Enabled, payload.ApprovalScopePaths
	capability, ok := definition.Capability(payload.CapabilityKey)
	if !ok || !capability.CallableByAgent() || !capability.AllowsApprovalPolicy(payload.ApprovalPolicy) {
		return integrationpackage.Capability{}, errs.ErrInvalid
	}
	if payload.ApprovalPolicy == "HUMAN_SCOPED" && enabled {
		if capability.ValidateApprovalScopePaths(paths) != nil {
			return integrationpackage.Capability{}, errs.ErrInvalid
		}
	} else if len(paths) != 0 {
		return integrationpackage.Capability{}, errs.ErrInvalid
	}
	return capability, nil
}

// readAssistantGrantPlanSnapshot reads the fixed organization/connection/assistant/capability
// tuple through the existing query in the supplied transaction. Scan failures, including NoRows,
// become Unavailable; partial tuples are discarded. No lock, owner resolution or commit is added.
// readAssistantGrantPlanSnapshot читает фиксированный tuple организации, соединения, ассистента
// и возможности прежним запросом в переданной транзакции. Ошибки Scan, включая NoRows, дают
// Unavailable; частичный tuple отбрасывается. Новых locks, разрешения владельца или Commit нет.
func readAssistantGrantPlanSnapshot(ctx context.Context, tx pgx.Tx, organizationID, connectionRef, assistantRef, capabilityKey string) (assistantGrantPlanSnapshot, error) {
	var snapshot assistantGrantPlanSnapshot
	if err := tx.QueryRow(ctx, querySystemAssistantIntegrationGrantSnapshot, organizationID, connectionRef, assistantRef, capabilityKey).Scan(&snapshot.ref, &snapshot.version, &snapshot.policy, &snapshot.enabled, &snapshot.paths); err != nil {
		return assistantGrantPlanSnapshot{}, errs.ErrUnavailable
	}
	return snapshot, nil
}

// assistantGrantHydratedOperation creates the plan envelope from caller-owned server pins and
// the existing grant. It preserves explicit empty path slices and shared expected/target version
// pointer identity; no authorization, mutation, receipt, audit or event is performed.
// assistantGrantHydratedOperation строит оболочку плана из серверных привязок вызывающего и
// прежнего grant. Сохраняет явно пустые paths и общий указатель версии expected/target;
// авторизация, изменение, receipt, audit или event не выполняются.
func assistantGrantHydratedOperation(operation entity.AssistantPlanOperation, params map[string]any, existingEnabled bool, policy string, existingPaths []string, connectionRef, connectionName string, connectionVersion int64) entity.AssistantPlanOperation {
	before := cloneAssistantFields(params)
	before["enabled"], before["approvalPolicy"], before["approvalScopePaths"] = existingEnabled, policy, append([]string{}, existingPaths...)
	operation.Parameters, operation.Before, operation.After = params, before, cloneAssistantFields(params)
	version := connectionVersion
	operation.ExpectedVersion = &version
	operation.Target = entity.AssistantPlanTarget{Kind: "INTEGRATION_CONNECTION", Ref: connectionRef, Name: connectionName, Version: &version}
	operation.Action = "UPDATE"
	operation.Selected = true
	return operation
}

// assistantGrantPlanSnapshotsEqual compares the same four JSON projections used for grant
// freshness. Nil and numeric representation follow assistantJSONEqual; equality neither resolves
// an owner nor authorizes execution.
// assistantGrantPlanSnapshotsEqual сравнивает прежние четыре JSON-проекции свежести grant.
// Nil и числовые представления обрабатывает assistantJSONEqual; совпадение не разрешает
// владельца и не выдаёт полномочия исполнения.
func assistantGrantPlanSnapshotsEqual(fresh, original entity.AssistantPlanOperation) bool {
	return assistantJSONEqual(fresh.Before, original.Before) && assistantJSONEqual(fresh.After, original.After) && assistantJSONEqual(fresh.ExpectedVersion, original.ExpectedVersion) && assistantJSONEqual(fresh.Target, original.Target)
}

// assistantGrantFieldsEqual compares only the caller-specified ordered field set and stops on
// the first JSON mismatch. Scope-specific callers own the immutable/identity/stable key lists,
// error priority and stale-refresh behavior; the maps are never changed.
// assistantGrantFieldsEqual сравнивает только заданные вызывающим поля по порядку до первого
// JSON mismatch. Отдельные callers владеют списками immutable/identity/stable keys, приоритетом
// ошибок и stale refresh; maps не изменяются.
func assistantGrantFieldsEqual(left, right map[string]any, fields []string) bool {
	for _, field := range fields {
		if !assistantJSONEqual(left[field], right[field]) {
			return false
		}
	}
	return true
}
