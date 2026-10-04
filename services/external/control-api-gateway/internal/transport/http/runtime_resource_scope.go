package httptransport

import (
	"strings"

	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func runtimeResourceScopeKind(value string) generated.RuntimeResourceScopeKind {
	return generated.RuntimeResourceScopeKind(strings.TrimPrefix(value, "RUNTIME_RESOURCE_SCOPE_KIND_"))
}

func validRuntimeResourceScope(kind generated.RuntimeResourceScopeKind, organizationRef, projectRef string) bool {
	if !opaqueHTTPReference.MatchString(organizationRef) || len(organizationRef) > 96 {
		return false
	}
	switch kind {
	case generated.RuntimeResourceScopeKindORGANIZATION:
		return projectRef == ""
	case generated.RuntimeResourceScopeKindPROJECT:
		return opaqueHTTPReference.MatchString(projectRef) && len(projectRef) <= 96
	default:
		return false
	}
}
