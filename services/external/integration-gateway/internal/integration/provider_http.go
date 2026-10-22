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

// providerCall contains the validated provider binding, projected credential reference and bounded payload selected by a typed adapter, not an arbitrary external URL command.
// RU: providerCall содержит проверенную provider-привязку, ссылку credential и ограниченный payload типизированного адаптера, а не произвольную внешнюю URL-команду.
// JSON and multipart bodies are mutually exclusive; Client optionally selects an already configured specialized transport.
// RU: JSON и multipart body взаимоисключаемы; Client необязательно выбирает уже настроенный специализированный transport.
type providerCall struct {
	BaseURL, Method, Path, AuthScheme, AuthHeader, Username, EffectKey, IdempotencyHeader string
	Query                                                                                 url.Values
	Body                                                                                  any
	Credential                                                                            *CredentialRevision
	Capability                                                                            integrationpackage.Capability
	MultipartBody                                                                         []byte
	MultipartType                                                                         string
	Client                                                                                *http.Client
}

// callProvider returns only the bounded successful body from the common provider boundary, preserving its safe or unknown-outcome errors.
// RU: callProvider возвращает только ограниченное успешное тело общей provider-границы, сохраняя безопасные ошибки либо неопределённость результата.
func (adapter *Adapter) callProvider(ctx context.Context, call providerCall) ([]byte, error) {
	response, err := adapter.callProviderResponse(ctx, call)
	if err != nil {
		return nil, err
	}
	return response.Body, nil
}

// providerResponse carries a bounded successful body, copied headers and status so typed adapters can validate projections and pagination without a live response stream.
// RU: providerResponse передаёт ограниченное успешное тело, копию заголовков и статус для проверки проекций и пагинации без живого потока ответа.
type providerResponse struct {
	Body       []byte
	Header     http.Header
	StatusCode int
}

// callProviderResponse validates HTTPS base/path and OpenAPI auth bindings, loads exact credentials, and sends bounded requests with per-attempt deadlines.
// RU: callProviderResponse проверяет HTTPS base/path и OpenAPI auth-привязки, загружает точный credential и отправляет ограниченные запросы со сроком каждой попытки.
// GET/HEAD may retry selected statuses or transport errors under capability policy; other methods run once with transport replay disabled.
// RU: GET/HEAD могут повторять выбранные статусы либо transport-ошибки по политике capability; другие методы выполняются один раз с отключённым transport replay.
// Mutation transport failures, failed successful-body reads and server errors retain distinct UNKNOWN stages instead of implying safe resend.
// RU: Transport-ошибки изменения, ошибка чтения успешного тела и server-ошибки сохраняют отдельные этапы UNKNOWN, а не разрешают безопасный повтор.
// Every obtained response body closes before return or retry, credentials are cleared on exit, and only successful responses expose copied headers.
// RU: Каждое полученное тело закрывается до возврата либо повтора, credential очищается при выходе, а копия заголовков выдаётся только для успешного ответа.
func (adapter *Adapter) callProviderResponse(ctx context.Context, call providerCall) (*providerResponse, error) {
	baseURL, err := parseProviderBaseURL(call.BaseURL)
	if err != nil || call.Path == "" || !strings.HasPrefix(call.Path, "/") || strings.HasPrefix(call.Path, "//") {
		return nil, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	if binding := call.Capability.OpenAPI; binding != nil &&
		(call.AuthScheme != binding.AuthScheme || call.AuthHeader != binding.AuthHeader ||
			call.IdempotencyHeader != binding.IdempotencyHeader) {
		return nil, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	var credential []byte
	if call.AuthScheme == "NONE" {
		if call.Capability.OpenAPI == nil || call.Credential != nil {
			return nil, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
		}
	} else {
		credential, err = adapter.readCredential(ctx, call.Credential)
		if err != nil {
			return nil, err
		}
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
		case "NONE":
			// Отсутствие credential явно закреплено package binding.
		case "BEARER":
			request.Header.Set("Authorization", "Bearer "+string(credential))
		case "API_KEY_HEADER":
			if call.Capability.OpenAPI == nil || !integrationpackage.ValidOpenAPIOutboundHeader(call.AuthHeader) {
				cancel()
				return nil, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
			}
			request.Header.Set(call.AuthHeader, string(credential))
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
			if call.Capability.OpenAPI == nil {
				request.Header.Set("Idempotency-Key", call.EffectKey)
			} else if call.IdempotencyHeader != "" {
				request.Header.Set(call.IdempotencyHeader, call.EffectKey)
			}
		}
		// Запрещаем неявный повтор Transport даже при provider-native effect key.
		if mutation {
			request.GetBody = nil
			request.Body = io.NopCloser(bytes.NewReader(body))
			if len(body) == 0 {
				request.ContentLength = -1
			}
		}

		client := adapter.providerHTTPClient
		if call.Client != nil {
			client = call.Client
		}
		response, responseErr := client.Do(request)
		if responseErr != nil {
			cancel()
			if mutation {
				return nil, &UnknownOutcomeError{stage: "transport"}
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
				return nil, &UnknownOutcomeError{stage: "response_body"}
			}
			return nil, readErr
		}
		if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
			return &providerResponse{Body: responseBody, Header: response.Header.Clone(), StatusCode: response.StatusCode}, nil
		}
		if mutation && response.StatusCode >= http.StatusInternalServerError {
			return nil, &UnknownOutcomeError{stage: "provider_status"}
		}
		if attempt < attempts && retryableProviderStatus(response.StatusCode) &&
			waitProviderRetry(ctx, call.Capability, attempt, response.Header.Get("Retry-After")) {
			continue
		}
		return nil, statusError(response.StatusCode)
	}
	return nil, &SafeError{Code: "INTEGRATION_UNAVAILABLE"}
}

// parseProviderBaseURL accepts an HTTPS hostname at the default or explicit 443 port, rejecting literal IPs, userinfo, query, fragment and paths beyond root.
// RU: parseProviderBaseURL принимает HTTPS-имя с обычным либо явным портом 443, отклоняя IP-литералы, userinfo, query, fragment и путь глубже корня.
// It normalizes the root path but does not resolve DNS or replace the egress gateway's destination policy.
// RU: Функция нормализует корневой путь, но не разрешает DNS и не заменяет политику назначений egress gateway.
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

// retryableProviderStatus permits only throttling and selected transient gateway/service statuses; method and attempt-budget checks remain with the caller.
// RU: retryableProviderStatus допускает только ограничение частоты и выбранные временные gateway/service статусы; метод и бюджет попыток проверяет вызывающий.
func retryableProviderStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusBadGateway ||
		status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

// waitProviderRetry waits a capability-scaled delay clamped to 50 milliseconds through two seconds and returns false on cancellation.
// RU: waitProviderRetry ждёт вычисленную по capability паузу от 50 миллисекунд до двух секунд и возвращает false при отмене.
// Retry-After seconds or HTTP-date override the delay; invalid values or requested waits above two seconds decline the retry rather than sleeping longer.
// RU: Retry-After в секундах либо HTTP-date заменяет паузу; неверное значение либо ожидание свыше двух секунд запрещают повтор вместо долгого сна.
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

// decodeProviderJSON decodes exactly one JSON value and requires EOF, returning a safe error without exposing parser diagnostics.
// RU: decodeProviderJSON разбирает ровно одно JSON-значение и требует EOF, возвращая безопасную ошибку без parser-диагностики.
// It does not disallow unknown object fields or replace typed projection and capability-schema validation.
// RU: Функция не запрещает неизвестные поля объекта и не заменяет типизированную проекцию и проверку схемы capability.
func decodeProviderJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(target); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return nil
}

// providerResult requires a nonempty provider reference, JSON-encodes the typed projection and binds its exact bytes to the claimed effect/input.
// RU: providerResult требует непустую provider-ссылку, кодирует типизированную проекцию в JSON и связывает точные байты с назначенными effect/input.
// Marshal failure returns an empty result with a safe code; final size and output-schema checks remain in validateExecutionResult.
// RU: Ошибка marshal возвращает пустой результат с безопасным кодом; итоговые размер и output schema проверяет validateExecutionResult.
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
