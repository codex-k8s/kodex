package httptransport

import (
	"io"
	"net/http"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
)

// decodeMailboxJSON bounds the complete body before exact plain DTO decoding. Optional fields may
// be omitted but explicit null is invalid. Raw mutable bytes are cleared; immutable decoder/string
// copies are not claimed erased. Syntax rejection happens before any mailbox owner/client call.
// decodeMailboxJSON ограничивает полное тело до точного разбора обычного DTO. Необязательные поля
// можно пропустить, но явный null недопустим. Исходные изменяемые байты очищаются; стирание immutable
// копий decoder/string не заявляется. Синтаксис проверяется до любого вызова владельца mailbox.
func decodeMailboxJSON[T any](writer http.ResponseWriter, request *http.Request) (T, bool) {
	var result T
	raw, err := io.ReadAll(io.LimitReader(request.Body, maximumJSONBody+1))
	defer clear(raw)
	if err != nil || int64(len(raw)) > maximumJSONBody || internalrpcauth.DecodeExactJSON(raw, &result) != nil {
		writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
		return result, false
	}
	return result, true
}

// decodeMailboxMutation prepares exact mailbox body and mutation syntax without assigning actor,
// owner or permission. It never returns a partially decoded request after failure.
// decodeMailboxMutation готовит точное тело mailbox и синтаксис mutation, не назначая actor,
// владельца или permission. Частично разобранный запрос после ошибки не возвращается.
func decodeMailboxMutation[T any](writer http.ResponseWriter, request *http.Request, idempotency, etag string) (T, *cp.MutationContext, bool) {
	body, ok := decodeMailboxJSON[T](writer, request)
	if !ok {
		return body, nil, false
	}
	mutation, ok := requireMutation(writer, idempotency, etag)
	return body, mutation, ok
}
