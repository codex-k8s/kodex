// Package authorization преобразует только проверенный internal RPC context.
package authorization

import (
	"context"
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

const (
	expectedAudience   = "urn:kodex:internal-rpc:control-plane"
	expectedWorkloadID = "control-plane"
)

// TrustedPrincipal не разрешает включить доверенный профиль входным payload.
func TrustedPrincipal(ctx context.Context, fullMethod string) (value.Principal, error) {
	resolved, ok := ctx.Value(resolvedPrincipalKey{}).(resolvedPrincipal)
	if !ok || resolved.profile != transportprofile.TrustedCluster {
		return value.Principal{}, errors.New("trusted principal is unavailable")
	}
	return Principal(ctx, fullMethod)
}

func Principal(ctx context.Context, fullMethod string) (value.Principal, error) {
	if resolved, ok := ctx.Value(resolvedPrincipalKey{}).(resolvedPrincipal); ok {
		if resolved.method != fullMethod || resolved.principal.Validate() != nil {
			return value.Principal{}, errors.New("resolved authorization identity is invalid")
		}
		principal := resolved.principal
		principal.CredentialAMR = append([]string(nil), principal.CredentialAMR...)
		return principal, nil
	}
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
	if authenticatedAt := verified.GetCredentialAuthenticatedAt(); authenticatedAt != nil && authenticatedAt.IsValid() {
		principal.CredentialAuthenticatedAt = authenticatedAt.AsTime().UTC()
		principal.CredentialACR = strings.TrimSpace(verified.GetCredentialAcr())
		principal.CredentialAMR = append([]string(nil), verified.GetCredentialAmr()...)
	}
	if verified.GetAuthority().GetProject() != nil {
		principal.ProjectRef = verified.GetAuthority().GetProject().GetId()
	}
	if err := principal.Validate(); err != nil {
		return value.Principal{}, errors.New("verified authorization identity is invalid")
	}
	return principal, nil
}
