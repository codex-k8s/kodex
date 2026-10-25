// Package problemresponse writes the gateway's common HTTP problem body; caller-specific status classification, headers, authorization and retry policy remain outside it.
// Package problemresponse записывает общее HTTP-тело ошибки шлюза; классификация статуса, заголовки, авторизация и политика повторов остаются у вызывающей стороны.
package problemresponse

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// Write commits the supplied status, uses an optional writer Localize method for the title and encodes the six-field problem body with a new correlation ID. It neither sets headers nor retries a failed write; callers supply already classified safe codes and retryability.
// Write отправляет переданный статус, использует необязательный метод Localize writer для заголовка и кодирует шесть полей ошибки с новым correlation ID. Заголовки не меняются, ошибочная запись не повторяется; вызывающий передаёт уже классифицированные безопасные коды и признак повтора.
func Write(writer http.ResponseWriter, statusCode int, code string, retryable bool) {
	writer.WriteHeader(statusCode)
	correlation := uuid.NewString()
	observeProblem(writer, statusCode, code, correlation)
	title := http.StatusText(statusCode)
	if localizer, ok := writer.(interface{ Localize(string) string }); ok {
		title = localizer.Localize(code)
	}
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"type": "urn:kodex:problem:" + strings.ToLower(code), "title": title,
		"status": statusCode, "code": code, "correlationId": correlation, "retryable": retryable,
	})
}
