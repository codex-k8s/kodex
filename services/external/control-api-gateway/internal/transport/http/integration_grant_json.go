package httptransport

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// integrationGrantJSONBody limits exact decoding to the two declared plain owner-command DTOs.
// integrationGrantJSONBody ограничивает exact decoding двумя declared plain owner-command DTOs.
type integrationGrantJSONBody interface {
	generated.IntegrationGrantInput | generated.SystemAssistantIntegrationGrantInput
}

// decodeIntegrationGrantJSON preserves exact field intent before owner dispatch: byte/UTF-8,
// duplicate, case-alias, null and required-field failures return an empty value and no RPC.
// Optional omission remains valid; semantic scope, target and authorization belong to the owner.
// Mutable raw bytes are cleared without claiming erasure of immutable decoder/string copies.
// decodeIntegrationGrantJSON сохраняет exact field intent до owner dispatch: byte/UTF-8,
// duplicate, case-alias, null и required-field failures возвращают empty value без RPC.
// Optional omission допустим; semantic scope, target и authorization принадлежат owner.
// Mutable raw bytes очищаются без заявления о стирании immutable decoder/string copies.
func decodeIntegrationGrantJSON[T integrationGrantJSONBody](writer http.ResponseWriter, request *http.Request) (T, bool) {
	var decoded, zero T
	raw, err := io.ReadAll(io.LimitReader(request.Body, maximumJSONBody+1))
	defer clear(raw)
	var fields map[string]json.RawMessage
	valid := err == nil && int64(len(raw)) <= maximumJSONBody &&
		internalrpcauth.DecodeExactJSON(raw, &decoded) == nil && json.Unmarshal(raw, &fields) == nil
	for _, key := range []string{"capabilityKey", "approvalPolicy", "enabled"} {
		if _, present := fields[key]; !present {
			valid = false
		}
	}
	if _, system := any(decoded).(generated.SystemAssistantIntegrationGrantInput); system {
		if _, present := fields["connectionRef"]; !present {
			valid = false
		}
	}
	if !valid {
		writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
		return zero, false
	}
	return decoded, true
}
