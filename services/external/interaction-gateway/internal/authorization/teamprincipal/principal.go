// Package teamprincipal связывает verified internal RPC context с TeamPrincipal.
package teamprincipal

import (
	"context"
	"errors"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/authorityclient"
	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
	"github.com/google/uuid"
)

const (
	expectedAudience     = "urn:mattercodex:internal-rpc:interaction-gateway"
	expectedCaller       = "control-api-gateway"
	expectedCallerSPIFFE = "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-api-gateway"
	expectedTarget       = "interaction-gateway"
	expectedTargetSPIFFE = "spiffe://mattercodex.local/ns/mattercodex-system/sa/interaction-gateway"
	expectedContract     = uint32(1)
)

func Principal(ctx context.Context, fullMethod, operation, permission string) (entity.TeamPrincipal, error) {
	return principal(ctx, fullMethod, operation, permission, true)
}

// ReadinessPrincipal допускает глобальный owner health без выбранного project.
func ReadinessPrincipal(ctx context.Context, fullMethod, operation, permission string) (entity.TeamPrincipal, error) {
	return principal(ctx, fullMethod, operation, permission, false)
}

func principal(ctx context.Context, fullMethod, operation, permission string, projectRequired bool) (entity.TeamPrincipal, error) {
	verified, ok := authorityclient.VerifiedAuthorizationContext(ctx)
	if !ok || verified.GetContractVersion() != expectedContract || verified.GetAudience() != expectedAudience ||
		verified.GetCallerWorkloadId() != expectedCaller || verified.GetCallerSpiffeId() != expectedCallerSPIFFE ||
		verified.GetTargetWorkloadId() != expectedTarget || verified.GetTargetSpiffeId() != expectedTargetSPIFFE ||
		verified.GetFullMethod() != fullMethod || verified.GetOperationId() != operation || verified.GetPermission() != permission ||
		verified.GetJti() == "" || verified.GetPolicyRevision() == 0 || verified.GetSourceRevision() == 0 ||
		verified.GetKeySetRevision() == 0 || verified.GetSignerGeneration() == 0 || verified.GetAuthority() == nil {
		return entity.TeamPrincipal{}, errors.New("verified Mattermost team authorization context is invalid")
	}
	authority := verified.GetAuthority()
	return principalFromAuthority(authority, projectRequired)
}

func principalFromAuthority(authority *internalrpcauthorityv1.CallerAuthority, projectRequired bool) (entity.TeamPrincipal, error) {
	if authority.GetActorKind() != internalrpcauthorityv1.ActorKind_ACTOR_KIND_HUMAN ||
		authority.GetActor() == nil || authority.GetTenant() == nil ||
		!validProvenance(authority.GetActor().GetProvenance(), internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_OIDC_SESSION) ||
		!validProvenance(authority.GetTenant().GetProvenance(), internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_OIDC_SESSION) ||
		!validUUID(authority.GetActor().GetId()) || !validUUID(authority.GetTenant().GetId()) {
		return entity.TeamPrincipal{}, errors.New("verified Mattermost team owner authority is invalid")
	}
	projectID := ""
	if project := authority.GetProject(); project != nil {
		if !validProvenance(project.GetProvenance(), internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_DOMAIN_STATE) ||
			!validUUID(project.GetId()) {
			return entity.TeamPrincipal{}, errors.New("verified Mattermost team project authority is invalid")
		}
		projectID = project.GetId()
	} else if projectRequired {
		return entity.TeamPrincipal{}, errors.New("verified Mattermost team project authority is absent")
	}
	return entity.TeamPrincipal{
		ActorID: authority.GetActor().GetId(), OrganizationID: authority.GetTenant().GetId(),
		ProjectID: projectID,
	}, nil
}

func validUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed != uuid.Nil
}

func validProvenance(value *internalrpcauthorityv1.AuthorityProvenance,
	expected internalrpcauthorityv1.AuthoritySource,
) bool {
	return value != nil && value.GetSource() == expected && value.GetRevision() != 0 &&
		validDigest(value.GetDigestSha256())
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, symbol := range value {
		if symbol < '0' || symbol > '9' {
			if symbol < 'a' || symbol > 'f' {
				return false
			}
		}
	}
	return true
}
