package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// querySystemAssistantIntegrationGrantsOwner embeds the canonical nonarchived organization
// system-assistant identity/version and enabled/READY indicator. It does not check caller access
// or connection readiness.
// querySystemAssistantIntegrationGrantsOwner встраивает идентичность и версию канонического
// неархивного системного ассистента организации и признак enabled/READY. Не проверяет доступ
// вызывающего кода и готовность подключения.
//
//go:embed sql/system_assistant_integration_grants__owner.sql
var querySystemAssistantIntegrationGrantsOwner string

// systemAssistantIntegrationGrantTarget holds the resolved canonical system assistant
// ref/name/version and readiness flag for typed grant input construction. It carries no
// caller-supplied recipient or independent permission.
// systemAssistantIntegrationGrantTarget хранит разрешённые ссылку, имя, версию и готовность
// канонического системного ассистента для сборки типизированного входа grant. Не содержит
// назначенного клиентом получателя и самостоятельных полномочий.
type systemAssistantIntegrationGrantTarget struct {
	ref, name string
	version   int64
	ready     bool
}

// systemAssistantIntegrationGrantOwner requires organization role-image access and
// integration.manage on the exact connection before loading the canonical organization system
// assistant. It maps missing owner to NotFound and malformed identity/version to Unavailable,
// leaving enabled readiness enforcement to the input path.
// systemAssistantIntegrationGrantOwner требует доступ к образам ролей организации и
// integration.manage точного подключения до загрузки канонического системного ассистента
// организации. Отсутствие владельца даёт NotFound, повреждённые идентичность или версия —
// Unavailable; обязательную готовность при включении проверяет путь входа.
func (repository *Repository) systemAssistantIntegrationGrantOwner(ctx context.Context, tx pgx.Tx, current scope, connectionRef string) (systemAssistantIntegrationGrantTarget, error) {
	var target systemAssistantIntegrationGrantTarget
	if err := repository.requireOrganizationRoleImageAccess(ctx, tx, current); err != nil {
		return target, err
	}
	connection, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: connectionRef})
	if err != nil {
		return target, err
	}
	if err := repository.requireAccess(ctx, tx, current, "integration.manage", connection); err != nil {
		return target, err
	}
	err = tx.QueryRow(ctx, querySystemAssistantIntegrationGrantsOwner, current.organizationID).Scan(&target.ref, &target.name, &target.version, &target.ready)
	if errors.Is(err, pgx.ErrNoRows) {
		return target, errs.ErrNotFound
	}
	if err != nil || target.ref == "" || target.version < 1 {
		return target, errs.ErrUnavailable
	}
	return target, nil
}

// systemAssistantIntegrationGrantInput validates typed policy and connection/capability, resolves
// the canonical authorized system assistant and requires owner/connection readiness when enabling.
// It loads the exact package, checks callable capability/policy and permits scope paths only for
// enabled HUMAN_SCOPED, returning ordinary IntegrationGrantInput bound to the server-selected
// agent. It does not persist a grant or commit.
// systemAssistantIntegrationGrantInput проверяет типизированные политику, подключение и
// capability, разрешает канонического допустимого системного ассистента и требует готовность
// владельца и подключения при включении. Загружает точный пакет, проверяет capability и политику и
// допускает scoped-пути только для включённого HUMAN_SCOPED, возвращая обычный
// IntegrationGrantInput с выбранным сервером агентом. Не сохраняет grant и не завершает
// транзакцию.
func (repository *Repository) systemAssistantIntegrationGrantInput(ctx context.Context, tx pgx.Tx, current scope, raw any) (command.IntegrationGrantInput, systemAssistantIntegrationGrantTarget, error) {
	payload, ok := raw.(command.SystemAssistantIntegrationGrantInput)
	if !ok || payload.ConnectionRef == "" || payload.CapabilityKey == "" || !validIntegrationApprovalPolicy(payload.ApprovalPolicy) {
		return command.IntegrationGrantInput{}, systemAssistantIntegrationGrantTarget{}, errs.ErrInvalid
	}
	target, err := repository.systemAssistantIntegrationGrantOwner(ctx, tx, current, payload.ConnectionRef)
	if err != nil {
		return command.IntegrationGrantInput{}, target, err
	}
	if payload.Enabled && !target.ready {
		return command.IntegrationGrantInput{}, target, errs.ErrConflict
	}
	var version int64
	var key, definitionVersion, digest string
	var ready bool
	err = tx.QueryRow(ctx, querySystemAssistantIntegrationGrantsConnection, current.organizationID, payload.ConnectionRef).Scan(&version, &key, &definitionVersion, &digest, &ready)
	if errors.Is(err, pgx.ErrNoRows) {
		return command.IntegrationGrantInput{}, target, errs.ErrNotFound
	}
	if err != nil {
		return command.IntegrationGrantInput{}, target, errs.ErrUnavailable
	}
	if payload.Enabled && !ready {
		return command.IntegrationGrantInput{}, target, errs.ErrConflict
	}
	definition, err := repository.integrationPackage(ctx, tx, current.organizationID, payload.ConnectionRef, key, definitionVersion, digest)
	if err != nil {
		return command.IntegrationGrantInput{}, target, err
	}
	capability, exists := definition.Capability(payload.CapabilityKey)
	if !exists || !capability.CallableByAgent() || !capability.AllowsApprovalPolicy(payload.ApprovalPolicy) {
		return command.IntegrationGrantInput{}, target, errs.ErrInvalid
	}
	if payload.Enabled && payload.ApprovalPolicy == "HUMAN_SCOPED" {
		if capability.ValidateApprovalScopePaths(payload.ApprovalScopePaths) != nil {
			return command.IntegrationGrantInput{}, target, errs.ErrInvalid
		}
	} else if len(payload.ApprovalScopePaths) != 0 {
		return command.IntegrationGrantInput{}, target, errs.ErrInvalid
	}
	return command.IntegrationGrantInput{ConnectionRef: payload.ConnectionRef, CapabilityKey: payload.CapabilityKey, AgentRef: target.ref,
		ApprovalPolicy: payload.ApprovalPolicy, ApprovalScopePaths: payload.ApprovalScopePaths, Enabled: payload.Enabled}, target, nil
}
