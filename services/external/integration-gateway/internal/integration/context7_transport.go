package integration

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"time"
)

const (
	context7Origin        = "https://mcp.context7.com"
	context7Endpoint      = context7Origin + "/mcp"
	context7Proxy         = "http://egress-gateway.kodex-system.svc.cluster.local:8080"
	context7RequestBytes  = 16 << 10
	context7ResponseBytes = 512 << 10
	context7TextBytes     = 32 << 10
	context7Deadline      = 30 * time.Second
)

func newContext7HTTPClient(config Config) (*http.Client, error) {
	if config.ProxyURL != context7Proxy || config.Timeout < time.Second || config.Timeout > 2*time.Minute {
		return nil, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	proxy, _ := url.Parse(context7Proxy)
	return &http.Client{
		Transport: &http.Transport{
			Proxy:               http.ProxyURL(proxy),
			TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS13, ServerName: "mcp.context7.com"},
			TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: min(config.Timeout, context7Deadline),
			MaxResponseHeaderBytes: 16 << 10, MaxIdleConns: 2, MaxIdleConnsPerHost: 2,
			MaxConnsPerHost: 2, IdleConnTimeout: 30 * time.Second,
		},
		Timeout:       min(config.Timeout, context7Deadline),
		CheckRedirect: func(*http.Request, []*http.Request) error { return &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"} },
	}, nil
}

// context7Transport не принимает адреса, методы или учётные данные от агента.
type context7Transport struct {
	base       http.RoundTripper
	credential string
}

func (transport context7Transport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL == nil || request.URL.String() != context7Endpoint || request.Host != "" && request.Host != "mcp.context7.com" ||
		(request.Method != http.MethodPost && request.Method != http.MethodDelete) {
		return nil, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	clone.Header.Del("Authorization")
	clone.Header.Del("Cookie")
	clone.Header.Set("CONTEXT7_API_KEY", transport.credential)
	method := ""
	if request.Method == http.MethodPost && request.Body == nil || request.Method == http.MethodDelete && request.Body != nil {
		return nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	if request.Body != nil {
		body, err := io.ReadAll(io.LimitReader(request.Body, context7RequestBytes+1))
		_ = request.Body.Close()
		if err != nil || len(body) > context7RequestBytes {
			return nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		var envelope struct {
			Method string `json:"method"`
		}
		if json.Unmarshal(body, &envelope) != nil || !context7MethodAllowed(envelope.Method) {
			return nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		method = envelope.Method
		clone.Body = io.NopCloser(bytes.NewReader(body))
		clone.ContentLength = int64(len(body))
	}
	response, err := transport.base.RoundTrip(clone)
	if err != nil {
		return nil, &SafeError{Code: "INTEGRATION_UNAVAILABLE", Transient: true}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_ = response.Body.Close()
		return nil, statusError(response.StatusCode)
	}
	if request.Method == http.MethodPost {
		notification := method == "notifications/initialized" || method == "notifications/cancelled"
		mediaType, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
		if notification && response.StatusCode != http.StatusAccepted && response.StatusCode != http.StatusNoContent ||
			!notification && (response.StatusCode != http.StatusOK || mediaErr != nil || mediaType != "application/json" && mediaType != "text/event-stream") {
			_ = response.Body.Close()
			return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
	}
	if response.ContentLength > context7ResponseBytes {
		_ = response.Body.Close()
		return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	response.Body = &context7BoundedBody{ReadCloser: response.Body, remaining: context7ResponseBytes}
	return response, nil
}

func context7MethodAllowed(method string) bool {
	switch method {
	case "initialize", "notifications/initialized", "notifications/cancelled", "tools/list", "tools/call":
		return true
	default:
		return false
	}
}

type context7BoundedBody struct {
	io.ReadCloser
	remaining int64
}

func (body *context7BoundedBody) Read(buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}
	if body.remaining == 0 {
		var extra [1]byte
		count, err := body.ReadCloser.Read(extra[:])
		if count > 0 {
			return 0, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return 0, err
	}
	if int64(len(buffer)) > body.remaining {
		buffer = buffer[:body.remaining]
	}
	count, err := body.ReadCloser.Read(buffer)
	body.remaining -= int64(count)
	return count, err
}
