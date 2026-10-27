package platform

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/jackc/pgx/v5"
)

// queryProviderRenewalResolveProof embeds authority resolution from an exact request/owner digest
// and current authorized account/version/credential plus active system OWNER binding.
// Observe/validate permit authorized/unknown states; execution/authorize require
// claimed/authorized and an unexpired task.
// queryProviderRenewalResolveProof встраивает authority resolution по exact request/owner digest и
// current authorized account/version/credential с active system OWNER binding. Observe/validate
// допускают authorized/unknown states; execution/authorize требуют claimed/authorized и unexpired
// task.
//
//go:embed sql/provider_renewal__resolve_proof.sql
var queryProviderRenewalResolveProof string

// providerRenewalProofOperation recognizes exactly renewal execute, observe, authorize and
// validate operations. It is a closed operation classifier, not identity verification or task
// authority.
// providerRenewalProofOperation распознаёт ровно renewal execute, observe, authorize и validate
// operations. Это закрытый operation classifier, а не identity verification либо task authority.
func providerRenewalProofOperation(operation string) bool {
	return operation == port.ProviderCredentialRenewalOperation || operation == port.ProviderCredentialRenewalObserveOperation ||
		operation == port.ProviderCredentialRenewalAuthorizeOperation || operation == port.ProviderCredentialRenewalValidateOperation
}

// resolveProviderRenewalProof requires the operation-specific control-plane or secret-broker
// workload, no project scope and valid request digest before resolving proof identity. A
// five-second SQL read must return the same system actor/tenant and current task/owner pins; no
// rows is Forbidden, backend failure Unavailable. It returns organization version and fixed actor
// version without issuing a new task grant.
// resolveProviderRenewalProof требует operation-specific control-plane либо secret-broker
// workload, отсутствие project scope и valid request digest до разрешения proof identity.
// Пятисекундное SQL чтение обязано вернуть тот же system actor/tenant и current task/owner pins;
// no rows даёт Forbidden, backend failure — Unavailable. Возвращает organization version и fixed
// actor version без выдачи нового task grant.
func (repository *Repository) resolveProviderRenewalProof(ctx context.Context, input port.ProofPrincipalInput) (port.ProofAuthority, error) {
	ownerCall := input.Operation == port.ProviderCredentialRenewalAuthorizeOperation || input.Operation == port.ProviderCredentialRenewalValidateOperation
	caller := "control-plane"
	if ownerCall {
		caller = "secret-broker"
	}
	if !providerRenewalProofOperation(input.Operation) || input.CallerWorkload != caller || input.ProjectRef != "" || !validModelCatalogDigest(input.RequestDigestSHA256) {
		return port.ProofAuthority{}, errs.ErrForbidden
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	identity, err := repository.resolveProofIdentity(ctx, input)
	if err != nil {
		return port.ProofAuthority{}, err
	}
	read := input.Operation == port.ProviderCredentialRenewalObserveOperation || input.Operation == port.ProviderCredentialRenewalValidateOperation
	var authority port.ProofAuthority
	var updated time.Time
	err = repository.pool.QueryRow(ctx, queryProviderRenewalResolveProof, identity.OrganizationID, input.RequestDigestSHA256, ownerCall, read).Scan(&authority.ActorID, &authority.OrganizationID, &updated, &authority.OrganizationVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return port.ProofAuthority{}, errs.ErrForbidden
	}
	if err != nil {
		return port.ProofAuthority{}, errs.ErrUnavailable
	}
	if identity.ActorID != authority.ActorID || identity.OrganizationID != authority.OrganizationID {
		return port.ProofAuthority{}, errs.ErrForbidden
	}
	authority.ActorVersion = 1
	return authority, nil
}
