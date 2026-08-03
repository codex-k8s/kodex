// Package authorization связывает проверенный контекст #186 с доменным Principal.
package authorization

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/authorityclient"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

const (
	expectedAudience       = "urn:mattercodex:internal-rpc:control-plane"
	expectedWorkloadID     = "control-plane"
	expectedWorkloadSPIFFE = "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane"
)

// Principal возвращает только проверенную сервером идентичность и точную
// привязку метода.
func Principal(ctx context.Context, fullMethod string) (value.Principal, error) {
	verified, ok := authorityclient.VerifiedAuthorizationContext(ctx)
	if !ok || verified.GetContractVersion() != 1 ||
		verified.GetAudience() != expectedAudience ||
		verified.GetTargetWorkloadId() != expectedWorkloadID ||
		verified.GetTargetSpiffeId() != expectedWorkloadSPIFFE ||
		verified.GetFullMethod() != fullMethod ||
		verified.GetAuthority() == nil ||
		verified.GetAuthority().GetActor() == nil ||
		verified.GetAuthority().GetActor().GetProvenance() == nil ||
		verified.GetAuthority().GetTenant() == nil ||
		verified.GetPermission() == "" ||
		verified.GetCallerWorkloadId() == "" ||
		verified.GetCallerSpiffeId() == "" ||
		verified.GetPolicyRevision() == 0 ||
		verified.GetSourceRevision() == 0 ||
		verified.GetKeySetRevision() == 0 ||
		verified.GetSignerGeneration() == 0 ||
		!validDigest(verified.GetSourceDigestSha256()) {
		return value.Principal{}, errors.New("verified authorization context is invalid")
	}
	projectID := ""
	if verified.GetAuthority().GetProject() != nil {
		projectID = verified.GetAuthority().GetProject().GetId()
	}
	authoritySource := strings.TrimPrefix(
		verified.GetAuthority().GetActor().GetProvenance().GetSource().String(),
		"AUTHORITY_SOURCE_",
	)
	authorityReference := verified.GetAuthority().GetActor().GetProvenance().GetReference()
	authorityRevision := verified.GetAuthority().GetActor().GetProvenance().GetRevision()
	authorityGrantGeneration := uint64(0)
	parts := strings.Split(authorityReference, "/")
	if len(parts) == 3 {
		attempt, parseAttemptErr := strconv.ParseUint(parts[1], 10, 32)
		generation, parseGenerationErr := strconv.ParseUint(parts[2], 10, 64)
		if parseAttemptErr != nil || parseGenerationErr != nil ||
			attempt == 0 || generation == 0 ||
			generation != authorityRevision {
			return value.Principal{}, errors.New("application grant lineage is invalid")
		}
		authorityReference = parts[0]
		authorityRevision = attempt
		authorityGrantGeneration = generation
	}
	principal := value.Principal{
		ActorID:                  verified.GetAuthority().GetActor().GetId(),
		OrganizationID:           verified.GetAuthority().GetTenant().GetId(),
		ProjectID:                projectID,
		Permission:               verified.GetPermission(),
		CorrelationID:            verified.GetJti(),
		PolicyRevision:           verified.GetPolicyRevision(),
		AuthorityGeneration:      verified.GetSignerGeneration(),
		CallerWorkload:           verified.GetCallerWorkloadId(),
		CallerSPIFFEID:           verified.GetCallerSpiffeId(),
		AuthoritySource:          authoritySource,
		AuthorityReference:       authorityReference,
		AuthorityRevision:        authorityRevision,
		AuthorityDigest:          verified.GetAuthority().GetActor().GetProvenance().GetDigestSha256(),
		AuthorityGrantGeneration: authorityGrantGeneration,
	}
	if err := principal.Validate(); err != nil {
		return value.Principal{}, errors.New("verified authorization identity is invalid")
	}
	return principal, nil
}

func validDigest(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	for _, symbol := range value {
		if (symbol < '0' || symbol > '9') && (symbol < 'a' || symbol > 'f') {
			return false
		}
	}
	return true
}
