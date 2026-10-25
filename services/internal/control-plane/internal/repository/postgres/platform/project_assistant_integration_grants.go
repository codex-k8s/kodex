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

// changeProjectAssistantIntegrationGrant names the specialized PROJECT-assistant integration-grant
// plan operation. Recognizing the name is not admission; owner/purpose pins and canonical grant
// authorization must still be rechecked.
// changeProjectAssistantIntegrationGrant задаёт имя specialized PROJECT-assistant
// integration-grant plan operation. Распознавание имени не является admission; owner/purpose pins
// и canonical grant authorization перепроверяются отдельно.
const changeProjectAssistantIntegrationGrant = "CHANGE_PROJECT_ASSISTANT_INTEGRATION_GRANT"

// queryProjectAssistantIntegrationGrantOwner embeds current project/profile/assistant/exact
// purpose/active connection resolution under SHARE locks, authority-project scope and
// project.manage/agent.manage/integration.manage visibility. It returns version/package header
// pins rather than granting execution.
// queryProjectAssistantIntegrationGrantOwner встраивает current project/profile/assistant/exact
// purpose/active connection resolution под SHARE locks, authority-project scope и
// project.manage/agent.manage/integration.manage visibility. Возвращает version/package header
// pins, а не execution grant.
//
//go:embed sql/project_assistant_integration_grants__owner.sql
var queryProjectAssistantIntegrationGrantOwner string

// projectAssistantIntegrationGrantTarget holds server-resolved assistant/project/profile identity,
// versions/readiness and connection header for one owner-purpose boundary. These values are read
// pins used by specialized admission, not trusted payload locators or a grant.
// projectAssistantIntegrationGrantTarget хранит server-resolved assistant/project/profile
// identity, versions/readiness и connection header одной owner-purpose boundary. Это read pins для
// specialized admission, а не trusted payload locators либо grant.
type projectAssistantIntegrationGrantTarget struct {
	ref, name, projectID, projectRef, profileRef string
	version, profileVersion                      int64
	ready                                        bool
	connection                                   projectAssistantGrantConnectionHeader
}

// projectAssistantGrantConnectionHeader contains exact connection ref/name/version and definition
// key/version/digest from the owner query. It deliberately omits credentials and full connection
// projection; header validity alone does not establish package execution eligibility.
// projectAssistantGrantConnectionHeader содержит exact connection ref/name/version и definition
// key/version/digest owner query. Не включает credentials и full connection projection; header
// validity сама не устанавливает package execution eligibility.
type projectAssistantGrantConnectionHeader struct {
	ref, name, definitionKey, definitionVersion, definitionDigest string
	version                                                       int64
}

// projectAssistantIntegrationGrantOwner reads the exact assistant/connection purpose within
// current tenant/project and SQL access guards, then checks returned
// identity/version/profile/header shape. No row is NotFound, backend or corrupt shape Unavailable;
// callers must discard partial target on error and separately authorize the actual grant.
// projectAssistantIntegrationGrantOwner читает exact assistant/connection purpose внутри current
// tenant/project с SQL access guards, затем проверяет returned identity/version/profile/header
// shape. No row даёт NotFound, backend либо corrupt shape — Unavailable; caller отбрасывает
// partial target при error и отдельно авторизует actual grant.
func (repository *Repository) projectAssistantIntegrationGrantOwner(ctx context.Context, tx pgx.Tx, current scope, assistantRef, connectionRef string) (projectAssistantIntegrationGrantTarget, error) {
	var target projectAssistantIntegrationGrantTarget
	err := tx.QueryRow(ctx, queryProjectAssistantIntegrationGrantOwner, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID,
		"assistant_ref": assistantRef, "connection_ref": connectionRef, "authority_project": current.authorityProjectID,
	}).Scan(&target.ref, &target.name, &target.version, &target.ready, &target.projectID, &target.projectRef, &target.profileRef, &target.profileVersion,
		&target.connection.ref, &target.connection.name, &target.connection.version,
		&target.connection.definitionKey, &target.connection.definitionVersion, &target.connection.definitionDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return target, errs.ErrNotFound
	}
	if err != nil {
		return target, errs.ErrUnavailable
	}
	if target.ref != assistantRef || target.projectRef == "" || target.profileRef == "" || target.version < 1 || target.profileVersion < 1 ||
		!validProjectAssistantGrantConnectionHeader(target.connection, connectionRef) {
		return target, errs.ErrUnavailable
	}
	return target, nil
}

// validProjectAssistantGrantConnectionHeader requires exact nonempty connection ref, name,
// positive version and nonempty package key/version with valid digest syntax. It is a local shape
// check without access, readiness or current package lookup.
// validProjectAssistantGrantConnectionHeader требует exact nonempty connection ref, name, positive
// version и nonempty package key/version с valid digest syntax. Это local shape check без access,
// readiness либо current package lookup.
func validProjectAssistantGrantConnectionHeader(header projectAssistantGrantConnectionHeader, connectionRef string) bool {
	return connectionRef != "" && header.ref == connectionRef && header.name != "" && header.version > 0 &&
		header.definitionKey != "" && header.definitionVersion != "" && validAssistantDefinitionDigest(header.definitionDigest)
}

// projectAssistantIntegrationGrantInput validates the typed specialized payload, resolves current
// owner-purpose target and assigns AgentRef from that server result before canonical grant
// authorization. Approval scope paths are copied; errors may return canonical input/target for
// diagnostics, without applying or committing a grant.
// projectAssistantIntegrationGrantInput проверяет typed specialized payload, разрешает current
// owner-purpose target и назначает AgentRef из server result до canonical grant authorization.
// Approval scope paths копируются; errors могут возвращать canonical input/target для диагностики
// без применения либо commit grant.
func (repository *Repository) projectAssistantIntegrationGrantInput(ctx context.Context, tx pgx.Tx, current scope, raw any) (command.IntegrationGrantInput, projectAssistantIntegrationGrantTarget, error) {
	payload, ok := raw.(command.ProjectAssistantIntegrationGrantInput)
	if !ok || payload.AssistantRef == "" || payload.Grant.ConnectionRef == "" || payload.Grant.CapabilityKey == "" || !validIntegrationApprovalPolicy(payload.Grant.ApprovalPolicy) {
		return command.IntegrationGrantInput{}, projectAssistantIntegrationGrantTarget{}, errs.ErrInvalid
	}
	target, err := repository.projectAssistantIntegrationGrantOwner(ctx, tx, current, payload.AssistantRef, payload.Grant.ConnectionRef)
	if err != nil {
		return command.IntegrationGrantInput{}, target, err
	}
	canonical := command.IntegrationGrantInput{ConnectionRef: payload.Grant.ConnectionRef, CapabilityKey: payload.Grant.CapabilityKey,
		AgentRef: target.ref, Enabled: payload.Grant.Enabled, ApprovalPolicy: payload.Grant.ApprovalPolicy, ApprovalScopePaths: append([]string{}, payload.Grant.ApprovalScopePaths...)}
	// Тот же canonical admission, что у обычного адресного grant: package, readiness и policy.
	if err := repository.authorizeIntegrationGrant(ctx, tx, current, canonical); err != nil {
		return canonical, target, err
	}
	return canonical, target, nil
}

// projectAssistantGrantOwnerPins builds a fresh map of PROJECT assistant,
// organization/project/profile refs and agent/profile versions from resolved owner state. Pins
// support exact plan comparison; the map itself is neither a signature nor permission to apply the
// plan.
// projectAssistantGrantOwnerPins строит fresh map PROJECT assistant, organization/project/profile
// refs и agent/profile versions по resolved owner state. Pins нужны для exact plan comparison; сам
// map не является signature либо permission применить plan.
func projectAssistantGrantOwnerPins(target projectAssistantIntegrationGrantTarget, current scope) map[string]any {
	return map[string]any{"projectAssistantRef": target.ref, "assistantScope": "PROJECT", "scopeKind": "ORGANIZATION",
		"organizationRef": current.organizationRef, "projectRef": target.projectRef, "assistantProfileRef": target.profileRef,
		"agentVersion": target.version, "profileVersion": target.profileVersion}
}

// authorizeProjectAssistantIntegrationGrantPlan visits only the specialized operation kind,
// re-resolves each current owner-purpose target and compares the six identity/scope pins exactly.
// Mismatch is hidden as NotFound; version OCC and actual capability/policy application are not
// performed by this helper.
// authorizeProjectAssistantIntegrationGrantPlan обходит только specialized operation kind, заново
// разрешает current owner-purpose target и точно сравнивает шесть identity/scope pins. Mismatch
// скрывается как NotFound; version OCC и actual capability/policy application helper не выполняет.
func (repository *Repository) authorizeProjectAssistantIntegrationGrantPlan(ctx context.Context, tx pgx.Tx, current scope, operations []entity.AssistantPlanOperation) error {
	for _, operation := range operations {
		if operation.Type != changeProjectAssistantIntegrationGrant {
			continue
		}
		target, err := repository.projectAssistantIntegrationGrantOwner(ctx, tx, current, assistantString(operation.Parameters, "projectAssistantRef"), assistantString(operation.Parameters, "connectionRef"))
		if err != nil {
			return err
		}
		pins := projectAssistantGrantOwnerPins(target, current)
		for _, field := range []string{"projectAssistantRef", "assistantScope", "scopeKind", "organizationRef", "projectRef", "assistantProfileRef"} {
			if !assistantJSONEqual(operation.Parameters[field], pins[field]) {
				return errs.ErrNotFound
			}
		}
	}
	return nil
}
