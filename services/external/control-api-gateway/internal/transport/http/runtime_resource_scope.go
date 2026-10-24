package httptransport

import (
	"strings"

	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// runtimeResourceScopeKind removes only the canonical protobuf scope prefix before conversion to
// the public discriminator. Unknown values remain unknown and must pass separate scope validation.
// runtimeResourceScopeKind удаляет только канонический protobuf префикс области перед
// преобразованием в публичный вид. Неизвестное значение остаётся неизвестным и требует отдельной
// проверки области.
func runtimeResourceScopeKind(value string) generated.RuntimeResourceScopeKind {
	return generated.RuntimeResourceScopeKind(strings.TrimPrefix(value, "RUNTIME_RESOURCE_SCOPE_KIND_"))
}

// validRuntimeResourceScope requires a bounded organization ref, then an empty project for
// ORGANIZATION or a bounded nonempty project for PROJECT. It validates locator shape without
// proving either resource belongs to the authenticated caller.
// validRuntimeResourceScope требует ограниченный ref организации, затем пустой проект для
// ORGANIZATION либо ограниченный непустой проект для PROJECT. Проверяется форма адресации без
// доказательства принадлежности ресурсов аутентифицированному caller.
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
