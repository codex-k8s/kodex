package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

type providerCall struct {
	BaseURL, Method, Path, AuthScheme, Username, EffectKey string
	Query                                                  url.Values
	Body                                                   any
	Credential                                             *CredentialRevision
	Capability                                             integrationpackage.Capability
	MultipartBody                                          []byte
	MultipartType                                          string
}

func (adapter *Adapter) callProvider(ctx context.Context, call providerCall) ([]byte, error) {
	response, err := adapter.callProviderResponse(ctx, call)
	if err != nil {
		return nil, err
	}
	return response.Body, nil
}

type providerResponse struct {
	Body   []byte
	Header http.Header
}

func (adapter *Adapter) callProviderResponse(ctx context.Context, call providerCall) (*providerResponse, error) {
	baseURL, err := parseProviderBaseURL(call.BaseURL)
	if err != nil || call.Path == "" || !strings.HasPrefix(call.Path, "/") || strings.HasPrefix(call.Path, "//") {
		return nil, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	credential, err := adapter.readCredential(ctx, call.Credential)
	if err != nil {
		return nil, err
	}
	defer clear(credential)

	body := []byte(nil)
	if call.Body != nil {
		body, err = json.Marshal(call.Body)
		if err != nil || len(body) > maximumResponseBytes {
			return nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
	}
	if call.MultipartBody != nil {
		if call.Body != nil || len(call.MultipartBody) > maximumResponseBytes {
			return nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		body = call.MultipartBody
	}
	attempts := call.Capability.Execution.MaxAttempts
	if attempts < 1 {
		attempts = 1
	}
	mutation := call.Method != http.MethodGet && call.Method != http.MethodHead
	if mutation {
		attempts = 1
	}
	timeout := time.Duration(call.Capability.Execution.TimeoutSeconds) * time.Second
	if timeout <= 0 || timeout > adapter.timeout {
		timeout = adapter.timeout
	}

	for attempt := 1; attempt <= attempts; attempt++ {
		endpoint := *baseURL
		escapedPath := strings.TrimSuffix(endpoint.EscapedPath(), "/") + call.Path
		decodedPath, decodeErr := url.PathUnescape(escapedPath)
		if decodeErr != nil || !strings.HasPrefix(decodedPath, "/") {
			return nil, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
		}
		endpoint.Path = decodedPath
		endpoint.RawPath = escapedPath
		endpoint.RawQuery = call.Query.Encode()
		attemptContext, cancel := context.WithTimeout(ctx, timeout)
		request, requestErr := http.NewRequestWithContext(attemptContext, call.Method, endpoint.String(), bytes.NewReader(body))
		if requestErr != nil {
			cancel()
			return nil, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
		}
		request.Header.Set("Accept", "application/json")
		if len(body) > 0 {
			request.Header.Set("Content-Type", "application/json")
		}
		if call.MultipartBody != nil {
			request.Header.Set("Content-Type", call.MultipartType)
			request.Header.Set("X-Atlassian-Token", "nocheck")
		}
		switch call.AuthScheme {
		case "BEARER":
			request.Header.Set("Authorization", "Bearer "+string(credential))
		case "BASIC":
			if call.Username == "" {
				cancel()
				return nil, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
			}
			request.SetBasicAuth(call.Username, string(credential))
		default:
			cancel()
			return nil, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
		}
		if call.EffectKey != "" {
			request.Header.Set("Idempotency-Key", call.EffectKey)
		}
		// Запрещаем неявный повтор Transport даже при provider-native effect key.
		if mutation {
			request.GetBody = nil
			request.Body = io.NopCloser(bytes.NewReader(body))
			if len(body) == 0 {
				request.ContentLength = -1
			}
		}

		response, responseErr := adapter.providerHTTPClient.Do(request)
		if responseErr != nil {
			cancel()
			if mutation {
				return nil, &UnknownOutcomeError{}
			}
			if attempt < attempts && waitProviderRetry(ctx, call.Capability, attempt, "") {
				continue
			}
			return nil, &SafeError{Code: "INTEGRATION_UNAVAILABLE"}
		}
		responseBody, readErr := readBoundedResponse(response.Body)
		_ = response.Body.Close()
		cancel()
		if readErr != nil {
			if mutation && response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
				return nil, &UnknownOutcomeError{}
			}
			return nil, readErr
		}
		if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
			return &providerResponse{Body: responseBody, Header: response.Header.Clone()}, nil
		}
		if mutation && response.StatusCode >= http.StatusInternalServerError {
			return nil, &UnknownOutcomeError{}
		}
		if attempt < attempts && retryableProviderStatus(response.StatusCode) &&
			waitProviderRetry(ctx, call.Capability, attempt, response.Header.Get("Retry-After")) {
			continue
		}
		return nil, statusError(response.StatusCode)
	}
	return nil, &SafeError{Code: "INTEGRATION_UNAVAILABLE"}
}

func parseProviderBaseURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil ||
		parsed.Fragment != "" || parsed.RawQuery != "" || (parsed.Path != "" && parsed.Path != "/") ||
		(parsed.Port() != "" && parsed.Port() != "443") || net.ParseIP(parsed.Hostname()) != nil {
		return nil, errors.New("provider base URL is invalid")
	}
	parsed.Path = ""
	return parsed, nil
}

func retryableProviderStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusBadGateway ||
		status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

func waitProviderRetry(ctx context.Context, capability integrationpackage.Capability, attempt int, retryAfter string) bool {
	delay := time.Duration(capability.Execution.RetryBackoffMilliseconds) * time.Millisecond * time.Duration(attempt)
	if retryAfter != "" {
		if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds >= 0 {
			if seconds > 2 {
				return false
			}
			delay = time.Duration(seconds) * time.Second
		} else if until, err := http.ParseTime(retryAfter); err == nil {
			delay = time.Until(until)
			if delay > 2*time.Second {
				return false
			}
		} else {
			return false
		}
	}
	if delay < 50*time.Millisecond {
		delay = 50 * time.Millisecond
	}
	if delay > 2*time.Second {
		delay = 2 * time.Second
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func decodeProviderJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(target); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return nil
}

func providerResult(request Request, providerRef string, projection any) (Result, error) {
	if providerRef == "" {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	summary, err := json.Marshal(projection)
	if err != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return successfulResult(string(summary), request, providerRef), nil
}
