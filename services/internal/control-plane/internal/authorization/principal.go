// Package authorization связывает verified #186 context с доменным Principal.
package authorization

import (
	"context"
	"errors"
	"strings"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/authorityclient"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

const (
	expectedAudience       = "urn:mattercodex:internal-rpc:control-plane"
	expectedWorkloadID     = "control-plane"
	expectedWorkloadSPIFFE = "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane"
)

// Principal возвращает только server-verified identity и exact method binding.
func Principal(ctx context.Context, fullMethod string) (value.Principal, error) {
	verified, ok := authorityclient.VerifiedAuthorizationContext(ctx)
	if !ok || verified.GetContractVersion() != 1 ||
		verified.GetAudience() != expectedAudience ||
		verified.GetTargetWorkloadId() != expectedWorkloadID ||
		verified.GetTargetSpiffeId() != expectedWorkloadSPIFFE ||
		verified.GetFullMethod() != fullMethod ||
		verified.GetAuthority() == nil ||
		verified.GetAuthority().GetActor() == nil ||
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
	principal := value.Principal{
		ActorID:             verified.GetAuthority().GetActor().GetId(),
		OrganizationID:      verified.GetAuthority().GetTenant().GetId(),
		ProjectID:           projectID,
		Permission:          verified.GetPermission(),
		CorrelationID:       verified.GetJti(),
		PolicyRevision:      verified.GetPolicyRevision(),
		AuthorityGeneration: verified.GetSignerGeneration(),
		CallerWorkload:      verified.GetCallerWorkloadId(),
		CallerSPIFFEID:      verified.GetCallerSpiffeId(),
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
