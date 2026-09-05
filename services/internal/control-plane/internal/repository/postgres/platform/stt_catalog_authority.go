package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

const sttModelCatalogOperation = "platform.stt.model-catalog.get"

func (repository *Repository) ResolveProofAuthority(ctx context.Context, input platformrepo.ProofPrincipalInput) (platformrepo.ProofAuthority, error) {
	if input.Operation == sttModelCatalogOperation && (input.CallerWorkload != "control-api-gateway" || input.ProjectRef != "") {
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	authority, err := repository.resolveProofIdentity(ctx, input)
	if err != nil || input.Operation != sttModelCatalogOperation {
		return authority, err
	}
	principal, err := repository.ResolvePrincipal(ctx, value.Principal{ActorID: authority.ActorID, AuthorityTenant: authority.OrganizationID})
	if err != nil {
		return platformrepo.ProofAuthority{}, err
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return platformrepo.ProofAuthority{}, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return platformrepo.ProofAuthority{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Каталог adapter не требует enabled STT, но использует тот же ACL, что editor.
	if err := repository.requireAccess(ctx, tx, current, "organization.manage", organizationTarget(current.organizationRef)); err != nil {
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	return authority, nil
}
