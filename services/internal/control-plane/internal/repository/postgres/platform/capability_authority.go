package platform

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/access"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryEffectiveCapabilitiesGrantTarget embeds an exact tenant/agent grant to active connection
// lookup. It resolves the integration target for a separate integration.manage check, without
// requiring the grant itself to be enabled or proving actor authority.
// queryEffectiveCapabilitiesGrantTarget встраивает exact tenant/agent grant lookup активной
// connection. Разрешает integration target для отдельного integration.manage check, не требует
// enabled самого grant и не доказывает actor authority.
//
//go:embed sql/effective_capabilities_grant_target.sql
var queryEffectiveCapabilitiesGrantTarget string

// capabilityAuthority holds a freshly resolved access subject/bindings with
// organization/project/agent locators and one evaluation time. Its methods narrow capability
// classes; concrete tool commands still require exact resource permission checks.
// capabilityAuthority хранит freshly resolved access subject/bindings с organization/project/agent
// locators и единым evaluation time. Методы ограничивают capability classes; конкретные tool
// commands всё ещё требуют exact resource permission checks.
type capabilityAuthority struct {
	subject                               entity.AccessSubject
	bindings                              []entity.AccessBinding
	organizationRef, projectRef, agentRef string
	evaluatedAt                           time.Time
}

// capabilityAuthority resolves current actor subject and bindings in the caller transaction, then
// captures a UTC evaluation time with supplied target locators. It does not infer authority from
// those locators or issue bindings/grants.
// capabilityAuthority разрешает current actor subject и bindings в caller transaction, затем
// фиксирует UTC evaluation time с переданными target locators. Не выводит authority из locators и
// не выдаёт bindings/grants.
func (repository *Repository) capabilityAuthority(ctx context.Context, tx pgx.Tx, current scope, projectRef, agentRef string) (capabilityAuthority, error) {
	subject, err := repository.resolveAccessSubject(ctx, tx, current.organizationID, current.actorRef)
	if err != nil {
		return capabilityAuthority{}, err
	}
	bindings, err := repository.loadAccessBindings(ctx, tx, current.organizationID, subject)
	if err != nil {
		return capabilityAuthority{}, err
	}
	return capabilityAuthority{subject: subject.AccessSubject, bindings: bindings, organizationRef: current.organizationRef, projectRef: projectRef, agentRef: agentRef, evaluatedAt: time.Now().UTC()}, nil
}

// platformAllowed maps only closed platform capabilities to all required access permissions, using
// an exact instance when a target ref exists and resource-kind scope otherwise. STT uses
// organization scope; unknown keys deny. Passing this class-level filter never replaces command
// target authorization.
// platformAllowed отображает только closed platform capabilities во все required access
// permissions, используя exact instance при target ref и resource-kind scope иначе. STT использует
// organization scope; unknown keys отклоняются. Class-level filter не заменяет command target
// authorization.
func (authority capabilityAuthority) platformAllowed(key string) bool {
	resourceKind, resourceRef := "", ""
	var permissions []string
	switch key {
	case "platform.project.manage":
		resourceKind, resourceRef, permissions = "PROJECT", authority.projectRef, []string{"project.manage"}
	case "platform.agent.manage":
		resourceKind, resourceRef, permissions = "AGENT", authority.agentRef, []string{"agent.manage"}
	case "platform.run.launch", "platform.run.delegate":
		resourceKind, resourceRef, permissions = "AGENT", authority.agentRef, []string{"agent.launch"}
	case "platform.gate.resolve":
		resourceKind, permissions = "OWNER_GATE", []string{"gate.resolve"}
	case "platform.artifact.manage":
		resourceKind, permissions = "ARTIFACT", []string{"artifact.view", "artifact.download", "artifact.upload", "artifact.bind", "artifact.delete"}
	case "platform.schedule.manage":
		resourceKind, permissions = "SCHEDULE", []string{"schedule.manage"}
	case "platform.integration.grant":
		resourceKind, permissions = "INTEGRATION", []string{"integration.manage"}
	case "platform.stt.use":
		return authority.allowed("platform.stt.use", entity.AccessScope{Kind: "ORGANIZATION", ResourceKind: "ORGANIZATION", ResourceRef: authority.organizationRef})
	default:
		return false
	}
	target := entity.AccessScope{Kind: "RESOURCE_KIND", ProjectRef: authority.projectRef, ResourceKind: resourceKind}
	if resourceRef != "" {
		target.Kind, target.ResourceRef = "RESOURCE_INSTANCE", resourceRef
	}
	for _, permission := range permissions {
		if !authority.allowed(permission, target) {
			return false
		}
	}
	return true
}

// allowed evaluates one exact permission/target against the captured subject/bindings at the
// shared evaluation time with no additional role override. It is a pure access predicate and does
// not refresh authority or persist a grant.
// allowed вычисляет exact permission/target по сохранённым subject/bindings в общем evaluation
// time без дополнительного role override. Это pure access predicate, не обновляющий authority и не
// сохраняющий grant.
func (authority capabilityAuthority) allowed(permission string, target entity.AccessScope) bool {
	return access.Evaluate(authority.subject, permission, target, "", authority.bindings, authority.evaluatedAt).Allowed
}

// requireCapabilityGrantAuthority builds fresh capability authority and returns Forbidden when its
// closed platform mapping is not allowed. Subject/binding read errors propagate; no capability is
// inserted or command executed.
// requireCapabilityGrantAuthority строит fresh capability authority и возвращает Forbidden при
// недопустимом closed platform mapping. Subject/binding read errors передаются далее; capability
// не добавляется, command не выполняется.
func (repository *Repository) requireCapabilityGrantAuthority(ctx context.Context, tx pgx.Tx, current scope, projectRef, agentRef, key string) error {
	authority, err := repository.capabilityAuthority(ctx, tx, current, projectRef, agentRef)
	if err != nil {
		return err
	}
	if !authority.platformAllowed(key) {
		return errs.ErrForbidden
	}
	return nil
}

// authorizeInitialAgentCapabilities validates the bounded initial key set, then resolves each
// enabled capability and rechecks current actor grant authority for the project without an
// existing agent ref. Missing keys are NotFound; it validates an owner-confirmed creation path
// without writing grants or committing.
// authorizeInitialAgentCapabilities проверяет bounded initial key set, затем разрешает каждую
// enabled capability и заново проверяет current actor grant authority project без existing agent
// ref. Missing keys дают NotFound; проверяет owner-confirmed creation path без записи grants либо
// commit.
func (repository *Repository) authorizeInitialAgentCapabilities(ctx context.Context, tx pgx.Tx, current scope, projectRef string, keys []string) error {
	if !validInitialAgentCapabilities(keys) {
		return errs.ErrInvalid
	}
	for _, key := range keys {
		var enabledKey string
		if err := tx.QueryRow(ctx, queryCommandsChangeagentbindingSelectEnabledCapability, key).Scan(&enabledKey); errors.Is(err, pgx.ErrNoRows) {
			return errs.ErrNotFound
		} else if err != nil {
			return errs.ErrUnavailable
		}
		if err := repository.requireCapabilityGrantAuthority(ctx, tx, current, projectRef, "", enabledKey); err != nil {
			return err
		}
	}
	return nil
}

// validInitialAgentCapabilities accepts at most three distinct artifact-manage, run-delegate or
// run-launch keys, including an empty set. It checks shape only; enabled status, owner
// confirmation and access remain separate checks.
// validInitialAgentCapabilities принимает не более трёх разных artifact-manage, run-delegate либо
// run-launch keys, включая empty set. Проверяет только shape; enabled status, owner confirmation и
// access проверяются отдельно.
func validInitialAgentCapabilities(keys []string) bool {
	if len(keys) > 3 {
		return false
	}
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		switch key {
		case "platform.artifact.manage", "platform.run.delegate", "platform.run.launch":
		default:
			return false
		}
		if _, duplicate := seen[key]; duplicate {
			return false
		}
		seen[key] = struct{}{}
	}
	return true
}

// requireAgentIntegrationGrantAuthority resolves a tenant/agent-specific grant to its active
// connection before resolving that integration target and requiring integration.manage. No row is
// NotFound and backend errors Unavailable; a grant ref alone cannot authorize its reassignment.
// requireAgentIntegrationGrantAuthority разрешает tenant/agent-specific grant в active connection
// до разрешения integration target и требования integration.manage. No row даёт NotFound, backend
// errors — Unavailable; одного grant ref недостаточно для разрешения его переназначения.
func (repository *Repository) requireAgentIntegrationGrantAuthority(ctx context.Context, tx pgx.Tx, current scope, agentRef, grantRef string) error {
	var connectionRef string
	if err := tx.QueryRow(ctx, queryEffectiveCapabilitiesGrantTarget, current.organizationID, grantRef, agentRef).Scan(&connectionRef); errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	} else if err != nil {
		return errs.ErrUnavailable
	}
	target, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: connectionRef})
	if err != nil {
		return err
	}
	return repository.requireAccess(ctx, tx, current, "integration.manage", target)
}
