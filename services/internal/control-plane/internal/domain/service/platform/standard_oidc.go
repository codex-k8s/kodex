package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

// ResolveStandardOIDCIdentity delegates verified standard evidence to the configured durable owner port, without accepting caller-selected authority.
// ResolveStandardOIDCIdentity передаёт verified standard evidence настроенному durable owner port без caller-selected authority.
func (service *Service) ResolveStandardOIDCIdentity(ctx context.Context, input platformrepo.StandardOIDCIdentity) (platformrepo.StandardOIDCBinding, error) {
	owner, ok := service.repository.(platformrepo.StandardOIDCIdentityOwner)
	if !ok {
		return platformrepo.StandardOIDCBinding{}, errs.ErrUnavailable
	}
	return owner.ResolveStandardOIDCIdentity(ctx, input)
}

// StandardOIDCSession returns only the current authenticated caller's session; closure reads are bound to a separate exact method permission.
// StandardOIDCSession возвращает лишь сессию текущего authenticated caller; closure reads привязаны к отдельному exact method permission.
func (service *Service) StandardOIDCSession(ctx context.Context, principal value.Principal, closure bool) (platformrepo.StandardOIDCBinding, error) {
	permission := platformrepo.StandardOIDCResolveOperation
	if closure {
		permission = platformrepo.StandardOIDCClosureOperation
	}
	if !standardOIDCSelfPrincipal(principal, permission) {
		return platformrepo.StandardOIDCBinding{}, errs.ErrForbidden
	}
	reader, ok := service.repository.(platformrepo.StandardOIDCSessionReader)
	if !ok {
		return platformrepo.StandardOIDCBinding{}, errs.ErrUnavailable
	}
	binding, err := reader.ReadStandardOIDCSession(ctx, principal.AuthorityTenant, principal.ActorID, principal.CredentialRevision, closure)
	if err != nil {
		return platformrepo.StandardOIDCBinding{}, err
	}
	if !standardOIDCBindingMatchesPrincipal(binding, principal) {
		return platformrepo.StandardOIDCBinding{}, errs.ErrForbidden
	}
	return binding, nil
}

// RevokeOwnStandardOIDCSession derives every selector from the exact verified principal and returns the authoritative tombstone after the atomic revoke/audit.
// The same request safely reconciles a lost response without creating another session or audit effect.
// RevokeOwnStandardOIDCSession выводит все selectors из exact verified principal и возвращает authoritative tombstone после атомарного revoke/audit.
// Тот же запрос безопасно сверяет потерянный ответ без создания новой сессии и audit effect.
func (service *Service) RevokeOwnStandardOIDCSession(ctx context.Context, principal value.Principal) (platformrepo.StandardOIDCBinding, error) {
	if !standardOIDCSelfPrincipal(principal, platformrepo.StandardOIDCRevokeOperation) {
		return platformrepo.StandardOIDCBinding{}, errs.ErrForbidden
	}
	reader, ok := service.repository.(platformrepo.StandardOIDCSessionReader)
	if !ok {
		return platformrepo.StandardOIDCBinding{}, errs.ErrUnavailable
	}
	if err := service.RevokeStandardOIDCSession(ctx, principal.AuthorityTenant, principal.ActorID, principal.CredentialRevision); err != nil {
		return platformrepo.StandardOIDCBinding{}, err
	}
	binding, err := reader.ReadStandardOIDCSession(ctx, principal.AuthorityTenant, principal.ActorID, principal.CredentialRevision, true)
	if err != nil {
		return platformrepo.StandardOIDCBinding{}, err
	}
	if !standardOIDCBindingMatchesPrincipal(binding, principal) || binding.State != "REVOKED" {
		return platformrepo.StandardOIDCBinding{}, errs.ErrUnavailable
	}
	return binding, nil
}

// standardOIDCSelfPrincipal admits only a current exact-operation gateway principal without project selectors or an unbounded credential revision.
// standardOIDCSelfPrincipal допускает лишь current exact-operation gateway principal без project selectors и неограниченной credential revision.
func standardOIDCSelfPrincipal(principal value.Principal, permission string) bool {
	actor, actorErr := uuid.Parse(principal.ActorID)
	organization, organizationErr := uuid.Parse(principal.AuthorityTenant)
	return principal.Validate() == nil && principal.CallerWorkload == "control-api-gateway" && principal.ProjectRef == "" &&
		principal.Permission == permission && principal.CredentialRevision > 0 && principal.CredentialRevision <= 1<<53-1 &&
		actorErr == nil && organizationErr == nil && actor != uuid.Nil && organization != uuid.Nil && actor.String() == principal.ActorID && organization.String() == principal.AuthorityTenant
}

// standardOIDCBindingMatchesPrincipal prevents a defective reader from returning another actor/tenant/session under an otherwise admitted self operation.
// standardOIDCBindingMatchesPrincipal не позволяет ошибочному reader вернуть другого actor/tenant/session внутри допущенной self operation.
func standardOIDCBindingMatchesPrincipal(binding platformrepo.StandardOIDCBinding, principal value.Principal) bool {
	return binding.ActorID == principal.ActorID && binding.OrganizationID == principal.AuthorityTenant && binding.Revision == principal.CredentialRevision
}

// RevokeStandardOIDCSession delegates an already owner-resolved self-session tuple; this wrapper does not derive it from a business payload.
// RevokeStandardOIDCSession передаёт уже owner-resolved self-session tuple; wrapper не выводит его из business payload.
func (service *Service) RevokeStandardOIDCSession(ctx context.Context, organizationID, actorID string, revision uint64) error {
	owner, ok := service.repository.(platformrepo.StandardOIDCIdentityOwner)
	if !ok {
		return errs.ErrUnavailable
	}
	return owner.RevokeStandardOIDCSession(ctx, organizationID, actorID, revision)
}
