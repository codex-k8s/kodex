package platform

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (repository *Repository) validRuntimeProjectionAuthority(authority platformrepo.CredentialProjectionAuthority, recovery bool) bool {
	if authority.RPCProfile == "" {
		return !repository.trustedCluster && validRuntimeProjectionAuthority(authority)
	}
	if !repository.trustedCluster || authority.RPCProfile != transportprofile.TrustedCluster ||
		authority.CallerWorkloadID != "runtime-controller" ||
		(authority.CallerFullMethod != runtimeProjectionMethod && authority.CallerFullMethod != assistantProjectionMethod) {
		return false
	}
	if !recovery {
		// Первый resolve не принимает назначенный caller scope или срок.
		return authority == (platformrepo.CredentialProjectionAuthority{
			RPCProfile: transportprofile.TrustedCluster, CallerWorkloadID: "runtime-controller", CallerFullMethod: authority.CallerFullMethod,
		})
	}
	now := time.Now().UTC()
	return authority.ProofJTI == "" && uuid.Validate(authority.ActorID) == nil && uuid.Validate(authority.TenantID) == nil &&
		((authority.CallerFullMethod == runtimeProjectionMethod && uuid.Validate(authority.ProjectID) == nil) ||
			(authority.CallerFullMethod == assistantProjectionMethod && authority.ProjectID == "")) &&
		authority.SourceRevision > 0 && validRuntimeSecretSHA256(authority.SourceDigestSHA256) &&
		authority.CallerCredentialRevision > 0 && authority.CallerCredentialRevision <= 9007199254740991 &&
		authority.ExpiresAt.After(now) && !authority.ExpiresAt.After(now.Add(5*time.Minute))
}

// Scope и поколение читаются в том же snapshot, что provider и runtime secrets.
// Следующий owner query сохраняет все предикаты lease/fence/attempt/lineage.
func (repository *Repository) resolveTrustedProjectionAuthority(ctx context.Context, tx pgx.Tx, organizationID string, input platformrepo.RuntimeCredentialProjectionInput) (platformrepo.CredentialProjectionAuthority, error) {
	result := platformrepo.CredentialProjectionAuthority{
		RPCProfile: transportprofile.TrustedCluster, CallerWorkloadID: "runtime-controller", CallerFullMethod: input.Authority.CallerFullMethod,
		SourceRevision: uint64(input.Generation), SourceDigestSHA256: input.RuntimeRevisionDigest,
	}
	err := tx.QueryRow(ctx, queryCredentialProjectionTrustedAuthority, pgx.StrictNamedArgs{
		"organization_id": organizationID, "lease_ref": input.LeaseRef, "workload_instance": input.WorkloadInstance,
		"generation": input.Generation, "runtime_revision_ref": input.RuntimeRevisionRef, "runtime_revision_digest": input.RuntimeRevisionDigest,
	}).Scan(&result.ActorID, &result.TenantID, &result.ProjectID, &result.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrNotFound
	}
	if err != nil {
		return result, errs.ErrUnavailable
	}
	err = tx.QueryRow(ctx, queryTrustedWorkloadGeneration, pgx.StrictNamedArgs{"workload_id": result.CallerWorkloadID}).Scan(&result.CallerCredentialRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrForbidden
	}
	if err != nil {
		return result, errs.ErrUnavailable
	}
	if input.Fence == "" {
		// Recovery не продлевает snapshot после renew и не принимает scope из него.
		if input.Authority.ExpiresAt.After(result.ExpiresAt) {
			return result, errs.ErrForbidden
		}
		result.ExpiresAt = input.Authority.ExpiresAt
		if result != input.Authority {
			return result, errs.ErrForbidden
		}
	}
	if !repository.validRuntimeProjectionAuthority(result, true) {
		return result, errs.ErrForbidden
	}
	return result, nil
}
