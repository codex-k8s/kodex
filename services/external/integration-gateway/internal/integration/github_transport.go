package integration

import (
	"bytes"
	"io"
	"net/http"
)

// Бюджет сырого SDK-ответа отличается от 64 КиБ безопасной проекции:
// GitHub повторяет provider metadata и полные описания в списках PR.
const maximumGitHubProviderResponseBytes = 2 << 20

// SDK не ограничивает тело до декодирования. Граница действует и для ошибок.
type githubBoundedTransport struct{ next http.RoundTripper }

func (transport githubBoundedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
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
	maximum := maximumGitHubProviderResponseBytes
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
