package integration

import (
	"bytes"
	"io"
	"net/http"
)

// maximumGitHubProviderResponseBytes bounds ordinary raw SDK payloads separately from the smaller safe-projection limit because lists include provider metadata and bodies.
// RU: maximumGitHubProviderResponseBytes отдельно ограничивает обычный сырой SDK payload, поскольку списки содержат provider-метаданные и body сверх меньшей безопасной проекции.
const maximumGitHubProviderResponseBytes = 2 << 20

// githubBoundedTransport enforces request-specific raw-body limits before SDK decoding, including the smaller error-response budget.
// RU: githubBoundedTransport ограничивает сырое тело по типу запроса до SDK-декодирования, включая меньший бюджет ошибочного ответа.
type githubBoundedTransport struct{ next http.RoundTripper }

// RoundTrip clones the request, validates its raw-response budget and disables automatic mutation replay without changing the caller's request object.
// RU: RoundTrip клонирует запрос, проверяет бюджет сырого ответа и отключает автоматический replay изменения, не меняя исходный объект запроса.
// It closes the upstream body and replaces it only with fully read bounded bytes; I/O or size failure yields no partial response to the SDK.
// RU: Функция закрывает upstream body и заменяет его только полностью прочитанными ограниченными байтами; ошибка I/O либо размера не выдаёт SDK частичный ответ.
func (transport githubBoundedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	maximum, valid := githubProviderResponseLimit(request)
	if !valid {
		return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		request.GetBody = nil
		if request.Body == nil || request.Body == http.NoBody {
			request.Body = io.NopCloser(bytes.NewReader(nil))
			request.ContentLength = -1
		}
	}
	response, err := transport.next.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		maximum = maximumResponseBytes
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, int64(maximum)+1))
	_ = response.Body.Close()
	if err != nil || len(body) > maximum {
		return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	return response, nil
}
