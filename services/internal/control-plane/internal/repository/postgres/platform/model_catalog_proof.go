package platform

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/model_catalog__resolve_proof.sql
var queryModelCatalogResolveProof string

func (repository *Repository) ResolveProviderModelCatalogProof(ctx context.Context, principal platformrepo.ProofPrincipalInput) (platformrepo.ProofAuthority, error) {
	if principal.CallerWorkload != "control-plane" || principal.Operation != platformrepo.ProviderModelCatalogOperation || principal.ProjectRef != "" || !validModelCatalogDigest(principal.RequestDigestSHA256) {
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	authority, err := repository.resolveProofIdentity(ctx, principal)
	if err != nil {
		return platformrepo.ProofAuthority{}, err
	}
	var resolved platformrepo.ProofAuthority
	var updatedAt time.Time
	err = repository.pool.QueryRow(ctx, queryModelCatalogResolveProof, principal.RequestDigestSHA256).Scan(&resolved.ActorID, &resolved.OrganizationID, &updatedAt, &resolved.OrganizationVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	if err != nil {
		return platformrepo.ProofAuthority{}, errs.ErrUnavailable
	}
	if resolved.ActorID != authority.ActorID || resolved.OrganizationID != authority.OrganizationID {
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	resolved.ActorVersion = 1
	return resolved, nil
}

func validModelCatalogDigest(digest string) bool {
	if len(digest) != 64 {
		return false
	}
	for _, value := range digest {
		if !(value >= 'a' && value <= 'f' || value >= '0' && value <= '9') {
			return false
		}
	}
	return true
}
