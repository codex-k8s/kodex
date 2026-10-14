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

//go:embed sql/provider_renewal__resolve_proof.sql
var queryProviderRenewalResolveProof string

func providerRenewalProofOperation(operation string) bool {
	return operation == port.ProviderCredentialRenewalOperation || operation == port.ProviderCredentialRenewalObserveOperation ||
		operation == port.ProviderCredentialRenewalAuthorizeOperation || operation == port.ProviderCredentialRenewalValidateOperation
}

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
