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
	// expectedAudience pins the control-plane audience required when converting a verified legacy
	// authorization context into a domain principal.
	// expectedAudience закрепляет audience control-plane при преобразовании проверенного legacy
	// authorization context в доменный principal.
	expectedAudience = "urn:kodex:internal-rpc:control-plane"
	// expectedWorkloadID pins the target workload accepted by the verified-context fallback; caller
	// payload cannot override this target.
	// expectedWorkloadID закрепляет target workload для fallback проверенного context; caller payload
	// не может заменить эту цель.
	expectedWorkloadID = "control-plane"
)

// TrustedPrincipal requires an already resolved trusted-cluster profile before the method-bound
// principal lookup, so payload or unverified metadata cannot enable that profile.
// TrustedPrincipal требует уже разрешённый профиль trusted-cluster до method-bound поиска
// principal, поэтому payload либо непроверенные metadata не включают этот профиль.
func TrustedPrincipal(ctx context.Context, fullMethod string) (value.Principal, error) {
	resolved, ok := ctx.Value(resolvedPrincipalKey{}).(resolvedPrincipal)
	if !ok || resolved.profile != transportprofile.TrustedCluster {
		return value.Principal{}, errors.New("trusted principal is unavailable")
	}
	return Principal(ctx, fullMethod)
}

// Principal returns a validated exact-method resolved identity or converts the verified authority
// context with fixed target/audience checks, copying AMR and treating project as a locator rather
// than self-assigned authority.
// Principal возвращает проверенную разрешённую identity точного метода либо преобразует verified
// authority context с фиксированными target и audience, копируя AMR и считая project locator, а не
// самостоятельно выданным полномочием.
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
