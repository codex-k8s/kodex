package app

import "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/session"

// browserIdentityProvider keeps the existing login/refresh ports together while the explicit authority mode selects one implementation at composition time.
// browserIdentityProvider объединяет existing login/refresh ports; явный authority mode выбирает одну implementation при composition.
type browserIdentityProvider interface {
	session.LoginProvider
	session.TokenRefresher
}
