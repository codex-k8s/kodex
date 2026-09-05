package integration

import (
	"bytes"
	"io"
	"net/http"
)

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
	body, err := readBoundedResponse(response.Body)
	_ = response.Body.Close()
	if err != nil {
		return nil, err
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	return response, nil
}
