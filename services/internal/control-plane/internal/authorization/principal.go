// Package authorization преобразует только проверенный internal RPC context.
package authorization

import (
	"context"
	"errors"
	"strings"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/authorityclient"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

const (
	expectedAudience   = "urn:mattercodex:internal-rpc:control-plane"
	expectedWorkloadID = "control-plane"
)

func Principal(ctx context.Context, fullMethod string) (value.Principal, error) {
	verified, ok := authorityclient.VerifiedAuthorizationContext(ctx)
	if !ok || verified.GetContractVersion() != 1 ||
		verified.GetAudience() != expectedAudience ||
		verified.GetTargetWorkloadId() != expectedWorkloadID ||
		verified.GetFullMethod() != fullMethod ||
		verified.GetAuthority() == nil || verified.GetAuthority().GetActor() == nil ||
		verified.GetAuthority().GetTenant() == nil ||
		verified.GetPermission() == "" || verified.GetJti() == "" ||
		verified.GetCallerWorkloadId() == "" {
		return value.Principal{}, errors.New("verified authorization context is invalid")
	}
	principal := value.Principal{
		ActorID:            strings.TrimSpace(verified.GetAuthority().GetActor().GetId()),
		AuthorityTenant:    strings.TrimSpace(verified.GetAuthority().GetTenant().GetId()),
		Permission:         verified.GetPermission(),
		CorrelationRef:     verified.GetJti(),
		CallerWorkload:     verified.GetCallerWorkloadId(),
		CredentialRevision: verified.GetCallerCredentialRevision(),
	}
	if verified.GetAuthority().GetProject() != nil {
		principal.ProjectRef = verified.GetAuthority().GetProject().GetId()
	}
	if err := principal.Validate(); err != nil {
		return value.Principal{}, errors.New("verified authorization identity is invalid")
	}
	return principal, nil
}
