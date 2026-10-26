package httptransport

import "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"

// runtimeSecretCreatePurpose preserves the optional default while forwarding explicit narrow credential custody.
// runtimeSecretCreatePurpose сохраняет optional default и передаёт явный узкий credential custody.
func runtimeSecretCreatePurpose(input generated.RuntimeSecretCreateInput) string {
	if input.Purpose == nil {
		return ""
	}
	return string(*input.Purpose)
}
