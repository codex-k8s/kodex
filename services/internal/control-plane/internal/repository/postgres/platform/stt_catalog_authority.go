package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// sttModelCatalogOperation identifies the protected STT model catalog operation, restricted to the
// control API gateway and organization scope. The operation name is classification, not a
// substitute for current owner ACL.
// sttModelCatalogOperation задаёт защищённую операцию STT model catalog, ограниченную control API
// gateway и областью организации. Имя операции — классификация, а не замена текущего ACL
// владельца.
const sttModelCatalogOperation = "platform.stt.model-catalog.get"

// ResolveProofAuthority dispatches specialized renewal/materialization/provider-catalog proof
// paths first, then enforces closed trusted/protected STT profile rules and resolves current
// identity. STT catalog/trusted operations recheck organization permission under the resolved
// principal; catalog access does not require enabled STT. It returns owner authority without
// issuing a credential, changing RPC profile or falling back after denial.
// ResolveProofAuthority сначала направляет специализированные
// renewal/materialization/provider-catalog proof пути, затем проверяет закрытые trusted/protected
// STT правила и разрешает текущую идентичность. STT catalog/trusted операции повторно проверяют
// организационное разрешение от разрешённого principal; каталог не требует включённого STT.
// Возвращает owner authority без выдачи credential, изменения RPC-профиля и fallback после отказа.
func (repository *Repository) ResolveProofAuthority(ctx context.Context, input platformrepo.ProofPrincipalInput) (platformrepo.ProofAuthority, error) {
	if providerRenewalProofOperation(input.Operation) {
		return repository.resolveProviderRenewalProof(ctx, input)
	}
	if runtimeMaterializationProofOperation(input.Operation) {
		return repository.resolveRuntimeMaterializationProof(ctx, input)
	}
	if input.Operation == platformrepo.ProviderModelCatalogOperation {
		return repository.ResolveProviderModelCatalogProof(ctx, input)
	}
	if input.Operation == sttModelCatalogOperation && (input.CallerWorkload != "control-api-gateway" || input.ProjectRef != "") {
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	permission, trustedSTT := platformrepo.TrustedSTTAuthorityPermission(input.CallerWorkload, input.Operation)
	if trustedSTT {
		if input.RPCProfile == "trusted-cluster" {
			if !repository.trustedCluster || input.ProjectRef != "" {
				return platformrepo.ProofAuthority{}, errs.ErrForbidden
			}
		} else {
			if repository.trustedCluster || input.Operation == platformrepo.TrustedSTTAuthorityOperation || input.Operation == platformrepo.TrustedSTTCatalogAuthorityOperation {
				return platformrepo.ProofAuthority{}, errs.ErrForbidden
			}
			// Существующие protected policy/projection сохраняют прежний owner path.
			trustedSTT = false
		}
	}
	authority, err := repository.resolveProofIdentity(ctx, input)
	if err != nil || (input.Operation != sttModelCatalogOperation && !trustedSTT) {
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
	if !trustedSTT {
		permission = "organization.manage"
	}
	if err := repository.requireAccess(ctx, tx, current, permission, organizationTarget(current.organizationRef)); err != nil {
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	return authority, nil
}
