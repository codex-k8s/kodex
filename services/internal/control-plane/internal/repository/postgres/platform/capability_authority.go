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

//go:embed sql/effective_capabilities_grant_target.sql
var queryEffectiveCapabilitiesGrantTarget string

type capabilityAuthority struct {
	subject                               entity.AccessSubject
	bindings                              []entity.AccessBinding
	organizationRef, projectRef, agentRef string
	evaluatedAt                           time.Time
}

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

// Возможность не заменяет exact permission на конкретном tool target.
// Эта проверка запрещает выдачу более широкого класса действий, чем разрешён actor.
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

func (authority capabilityAuthority) allowed(permission string, target entity.AccessScope) bool {
	return access.Evaluate(authority.subject, permission, target, "", authority.bindings, authority.evaluatedAt).Allowed
}

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
