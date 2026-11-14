package telegram

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"testing"
	"time"
)

// fixtureToken is synthetic and used only by in-process transports; no Telegram request is sent.
// fixtureToken синтетический и используется лишь in-process transports; запросы Telegram не отправляются.
const fixtureToken = "123456:synthetic_token_for_private_transport"

// fixtureTransport adapts a test callback without a network endpoint or global client.
// fixtureTransport адаптирует test callback без network endpoint либо глобального клиента.
type fixtureTransport func(*http.Request) (*http.Response, error)

// RoundTrip delegates one synthetic request, allowing exact retry-count assertions.
// RoundTrip передаёт один synthetic request для проверки точного числа retries.
func (next fixtureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return next(request)
}

// fixtureBody observes upstream cleanup on every provider response path.
// fixtureBody отслеживает upstream cleanup каждого пути provider response.
type fixtureBody struct {
	io.Reader
	closed bool
}

// Close records cleanup without external effects or another response allocation.
// Close фиксирует cleanup без внешних эффектов либо создания нового ответа.
func (body *fixtureBody) Close() error { body.closed = true; return nil }

// TestTokenPathIsPrivateAndTraceFree checks exact HTTPS/method/body and removes caller trace hooks and context values while retaining cancellation.
// TestTokenPathIsPrivateAndTraceFree проверяет точные HTTPS/method/body и отсутствие caller trace hooks/context values с сохранением отмены.
func TestTokenPathIsPrivateAndTraceFree(t *testing.T) {
	type privateKey struct{}
	calls := 0
	body := &fixtureBody{Reader: strings.NewReader(`{"ok":true,"result":{"id":123456,"is_bot":true}}`)}
	client, err := newAPIClient(fixtureToken, fixtureTransport(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Method != http.MethodPost || request.URL.Scheme != "https" || request.URL.Host != "api.telegram.org" || request.URL.Path != "/bot"+fixtureToken+"/getMe" || request.URL.RawQuery != "" || request.URL.User != nil || request.Header.Get("Authorization") != "" || request.Context().Value(privateKey{}) != nil || httptrace.ContextClientTrace(request.Context()) != nil {
			t.Fatal("request escaped exact private transport boundary")
		}
		raw, err := io.ReadAll(request.Body)
		if err != nil || string(raw) != "{}" {
			t.Fatal("unexpected provider request payload")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
	}), time.Second)
	if err != nil {
		t.Fatal("synthetic client creation failed")
	}
	ctx := context.WithValue(t.Context(), privateKey{}, "private-caller-value")
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { t.Error("caller trace observed token request") }})
	raw, err := client.call(ctx, operationIdentity, struct{}{})
	if err != nil || !strings.Contains(string(raw), "123456") || calls != 1 || !body.closed {
		t.Fatal("exact read or cleanup failed")
	}
}

// TestProviderRejectionsNeedExactEnvelope distinguishes proven no-effect flood waits from ambiguous outcomes without exposing descriptions or following redirects/migrations.
// TestProviderRejectionsNeedExactEnvelope отличает доказанный no-effect flood wait от неоднозначных исходов без descriptions и переходов по redirects/migrations.
func TestProviderRejectionsNeedExactEnvelope(t *testing.T) {
	for _, fixture := range []struct {
		name     string
		status   int
		body     string
		code     string
		noEffect bool
		retry    time.Duration
	}{
		{name: "flood", status: 429, body: `{"ok":false,"error_code":429,"description":"private provider text","parameters":{"retry_after":10}}`, code: errorRateLimited, noEffect: true, retry: 10 * time.Second},
		{name: "forbidden", status: 403, body: `{"ok":false,"error_code":403,"description":"private provider text"}`, code: errorProviderRejected, noEffect: true},
		{name: "migration", status: 400, body: `{"ok":false,"error_code":400,"parameters":{"migrate_to_chat_id":-1234567}}`, code: errorProviderRejected, noEffect: true},
		{name: "missing_ok", status: 400, body: `{"error_code":400}`, code: errorUnknown},
		{name: "null_ok", status: 400, body: `{"ok":null,"error_code":400}`, code: errorUnknown},
		{name: "case_alias", status: 400, body: `{"OK":false,"error_code":400}`, code: errorUnknown},
		{name: "duplicate_ok", status: 400, body: `{"ok":true,"ok":false,"error_code":400}`, code: errorUnknown},
		{name: "nested_duplicate", status: 400, body: `{"ok":false,"error_code":400,"parameters":{"retry_after":1,"retry_after":2}}`, code: errorUnknown},
		{name: "missing_wait", status: 429, body: `{"ok":false,"error_code":429}`, code: errorUnknown},
		{name: "oversized_wait", status: 429, body: `{"ok":false,"error_code":429,"parameters":{"retry_after":3601}}`, code: errorUnknown},
		{name: "negative_wait", status: 429, body: `{"ok":false,"error_code":429,"parameters":{"retry_after":-1}}`, code: errorUnknown},
		{name: "contradictory_result", status: 400, body: `{"ok":false,"error_code":400,"result":{"message_id":1}}`, code: errorUnknown},
		{name: "unexpected_field", status: 400, body: `{"ok":false,"error_code":400,"private_text":"private provider text"}`, code: errorUnknown},
		{name: "status_mismatch", status: 400, body: `{"ok":false,"error_code":403}`, code: errorUnknown},
		{name: "server_error", status: 500, body: `{"ok":false,"error_code":500}`, code: errorUnknown},
		{name: "redirect", status: 302, body: `{"ok":false,"error_code":302}`, code: errorUnknown},
		{name: "truncated_success", status: 200, body: `{"ok":true,"result":`, code: errorUnknown},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			calls := 0
			body := &fixtureBody{Reader: strings.NewReader(fixture.body)}
			client, err := newAPIClient(fixtureToken, fixtureTransport(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: fixture.status, Body: body}, nil
			}), time.Second)
			if err != nil {
				t.Fatal("synthetic client creation failed")
			}
			_, err = client.call(t.Context(), operationSend, struct{}{})
			var safe *SafeError
			if !errors.As(err, &safe) || safe.Code != fixture.code || ConfirmedNoEffect(err) != fixture.noEffect || safe.RetryAfter != fixture.retry || calls != 1 || !body.closed || strings.Contains(err.Error(), "private provider text") || strings.Contains(err.Error(), fixtureToken) {
				t.Fatal("unsafe provider classification, retry or cleanup")
			}
		})
	}
}

// TestTransportFailureNeverLeaksURLOrProvesNoEffect preserves UNKNOWN after possible write and discards raw URL-bearing errors while closing any accompanying body.
// TestTransportFailureNeverLeaksURLOrProvesNoEffect сохраняет UNKNOWN после возможной записи и отбрасывает raw errors с URL, закрывая возвращённый вместе с ними body.
func TestTransportFailureNeverLeaksURLOrProvesNoEffect(t *testing.T) {
	calls := 0
	body := &fixtureBody{Reader: strings.NewReader("private")}
	client, _ := newAPIClient(fixtureToken, fixtureTransport(func(request *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{Body: body}, &url.Error{Op: "Post", URL: request.URL.String(), Err: errors.New("private provider failure")}
	}), time.Second)
	_, err := client.call(t.Context(), operationSend, struct{}{})
	if err == nil || err.Error() != errorUnknown || ConfirmedNoEffect(err) || strings.Contains(err.Error(), fixtureToken) || calls != 1 || !body.closed {
		t.Fatal("raw transport error escaped or authorized another send")
	}
}

// TestPreflightCancellationAndInvalidTokenHaveNoDispatch rejects malformed path credentials and closed-method violations before I/O and links active request cancellation without caller trace state.
// TestPreflightCancellationAndInvalidTokenHaveNoDispatch отклоняет неверные path credentials и методы до I/O и связывает отмену активного request без caller trace state.
func TestPreflightCancellationAndInvalidTokenHaveNoDispatch(t *testing.T) {
	next := fixtureTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid preflight dispatched provider I/O")
		return nil, nil
	})
	for _, token := range []string{"", "0123456:synthetic_token_for_private_transport", "123456:synthetic/secret_segment", "123456:synthetic?secret_segment", "123456:short", "4503599627370496:synthetic_token_for_private_transport"} {
		if _, err := newAPIClient(token, next, time.Second); !ConfirmedNoEffect(err) {
			t.Fatal("malformed token did not close before dispatch")
		}
	}
	client, _ := newAPIClient(fixtureToken, next, time.Second)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := client.call(ctx, operationSend, struct{}{}); !ConfirmedNoEffect(err) {
		t.Fatal("pre-cancelled request could dispatch")
	}
	if _, err := client.call(t.Context(), operation("getUpdates"), struct{}{}); !ConfirmedNoEffect(err) {
		t.Fatal("inbound operation was accepted")
	}
	active, cancelActive := context.WithCancel(t.Context())
	client, _ = newAPIClient(fixtureToken, fixtureTransport(func(request *http.Request) (*http.Response, error) {
		cancelActive()
		<-request.Context().Done()
		return nil, request.Context().Err()
	}), time.Second)
	if _, err := client.call(active, operationSend, struct{}{}); err == nil || ConfirmedNoEffect(err) {
		t.Fatal("cancellation after dispatch authorized replay")
	}
}

// TestProviderBodyBoundClosesChunkedAndDeclaredOversize rejects excessive bytes before semantic use and closes readers on both paths.
// TestProviderBodyBoundClosesChunkedAndDeclaredOversize отклоняет чрезмерные bytes до семантического использования и закрывает readers обоих путей.
func TestProviderBodyBoundClosesChunkedAndDeclaredOversize(t *testing.T) {
	for _, length := range []int64{-1, maximumResponseBytes + 1} {
		body := &fixtureBody{Reader: strings.NewReader(strings.Repeat(" ", maximumResponseBytes+1))}
		client, _ := newAPIClient(fixtureToken, fixtureTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, ContentLength: length, Body: body}, nil
		}), time.Second)
		if raw, err := client.call(t.Context(), operationSend, struct{}{}); raw != nil || err == nil || ConfirmedNoEffect(err) || !body.closed {
			t.Fatal("provider body limit failed or authorized replay")
		}
	}
}
