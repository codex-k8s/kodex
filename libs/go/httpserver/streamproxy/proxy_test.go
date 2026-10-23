package streamproxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

// TestConstructorsPreserveExplicitTransportBudgets checks both registry budgets and a distinct caller-supplied set.
// It pins redirect refusal, TLS verification, handler/CA identity and no hidden default transport or listener startup.
// TestConstructorsPreserveExplicitTransportBudgets проверяет бюджеты обоих реестров и другой набор вызывающей стороны.
// Закрепляются отказ перенаправлению, проверка TLS, идентичность обработчика и CA и отсутствие скрытого транспорта или запуска listener.
func TestConstructorsPreserveExplicitTransportBudgets(t *testing.T) {
	t.Parallel()
	for _, budgets := range []Timeouts{
		{Header: 5 * time.Second, Stream: 15 * time.Minute, Idle: 30 * time.Second},
		{Header: time.Second, Stream: 9 * time.Second, Idle: 2 * time.Second},
	} {
		client := NewClient(budgets.Stream)
		if client.Timeout != budgets.Stream || client.Transport != nil || client.Jar != nil || client.CheckRedirect == nil {
			t.Fatal("client configuration changed")
		}
		if err := client.CheckRedirect(&http.Request{}, nil); err == nil || err.Error() != "redirect rejected" {
			t.Fatal("redirect rejection changed")
		}
		pool := x509.NewCertPool()
		handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
		server := NewMTLSServer(":0", handler, pool, budgets)
		if server.Addr != ":0" || reflect.ValueOf(server.Handler).Pointer() != reflect.ValueOf(handler).Pointer() ||
			server.ReadHeaderTimeout != budgets.Header || server.ReadTimeout != budgets.Stream ||
			server.WriteTimeout != budgets.Stream || server.IdleTimeout != budgets.Idle {
			t.Fatal("server configuration changed")
		}
		if server.TLSConfig == nil || server.TLSConfig.MinVersion != tls.VersionTLS13 ||
			server.TLSConfig.ClientAuth != tls.RequireAndVerifyClientCert || server.TLSConfig.ClientCAs != pool {
			t.Fatal("mTLS boundary changed")
		}
	}
}

// roundTripFunc supplies an in-memory HTTP exchange, preventing proxy unit tests from contacting a backend.
// roundTripFunc задаёт HTTP-обмен в памяти, не позволяя unit-тестам прокси обращаться к серверу.
type roundTripFunc func(*http.Request) (*http.Response, error)

// RoundTrip delegates exactly once to the test exchange without adding network, retry or redirect behavior.
// RoundTrip один раз передаёт запрос тестовому обмену, не добавляя сеть, повторы или перенаправления.
func (exchange roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return exchange(request)
}

// trackedResponseBody records response closure while retaining the supplied reader's exact bytes and errors.
// trackedResponseBody фиксирует закрытие ответа, сохраняя точные байты и ошибки переданного читателя.
type trackedResponseBody struct {
	io.Reader
	closed bool
}

// Close marks the synthetic response closed without external I/O or modifying its reader.
// Close отмечает закрытие искусственного ответа без внешнего ввода-вывода и изменения читателя.
func (body *trackedResponseBody) Close() error { body.closed = true; return nil }

// TestForwardPreservesCallerTargetBodyHeadersAndLimit checks upload versus nil read body and exact path/query copying.
// It verifies context, Authorization removal without incoming mutation, response headers/status/limit and body closure.
// TestForwardPreservesCallerTargetBodyHeadersAndLimit проверяет тело загрузки и nil для чтения и точное копирование пути и query.
// Проверяются контекст, удаление Authorization без изменения входа, заголовки, статус и лимит ответа и закрытие тела.
func TestForwardPreservesCallerTargetBodyHeadersAndLimit(t *testing.T) {
	t.Parallel()
	for _, withBody := range []bool{false, true} {
		t.Run(map[bool]string{false: "read", true: "upload"}[withBody], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			request := httptest.NewRequest(http.MethodPost, "https://untrusted.invalid/v2/repository/blobs/uploads/id?digest=sha256%3Aabc", strings.NewReader("upload"))
			request = request.WithContext(ctx)
			request.Header.Set("Authorization", "synthetic-secret")
			request.Header.Add("X-Preserve", "one")
			request.Header.Add("X-Preserve", "two")
			backend, err := url.Parse("http://127.0.0.1:5006/original?original=1")
			if err != nil {
				t.Fatal(err)
			}
			original := *backend
			responseBody := &trackedResponseBody{Reader: strings.NewReader("response-bytes")}
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(upstream *http.Request) (*http.Response, error) {
				calls++
				if upstream.Context() != ctx || upstream.URL.Scheme != "http" || upstream.URL.Host != "127.0.0.1:5006" ||
					upstream.URL.Path != request.URL.Path || upstream.URL.RawQuery != request.URL.RawQuery || upstream.Method != request.Method ||
					upstream.Header.Get("Authorization") != "" || !reflect.DeepEqual(upstream.Header.Values("X-Preserve"), []string{"one", "two"}) {
					t.Fatal("upstream request mapping changed")
				}
				if withBody {
					if upstream.Body == nil {
						t.Fatal("upload body missing")
					}
					value, err := io.ReadAll(upstream.Body)
					if err != nil || string(value) != "upload" {
						t.Fatal("upload bytes changed")
					}
				} else if upstream.Body != nil {
					t.Fatal("read proxy forwarded a body")
				}
				upstream.Header.Set("X-Preserve", "upstream-mutated")
				return &http.Response{StatusCode: http.StatusAccepted, Header: http.Header{"X-Result": {"one", "two"}}, Body: responseBody}, nil
			})}
			var body io.Reader
			if withBody {
				body = request.Body
			}
			writer := httptest.NewRecorder()
			writer.Header().Add("X-Result", "existing")
			Forward(writer, request, backend, body, client, 8)
			if calls != 1 || writer.Code != http.StatusAccepted || writer.Body.String() != "response" || !responseBody.closed ||
				!reflect.DeepEqual(writer.Header().Values("X-Result"), []string{"existing", "one", "two"}) {
				t.Fatal("response streaming, headers, closure or limit changed")
			}
			if *backend != original || request.Header.Get("Authorization") != "synthetic-secret" ||
				!reflect.DeepEqual(request.Header.Values("X-Preserve"), []string{"one", "two"}) {
				t.Fatal("forwarding mutated caller-owned target or headers")
			}
		})
	}
}

// TestForwardRejectsMalformedRequestAndTransportFailure keeps both safe 502 branches without leaking private causes.
// Invalid methods must fail before transport, while a failed synthetic exchange is attempted only once.
// TestForwardRejectsMalformedRequestAndTransportFailure сохраняет обе безопасные ветки 502 без раскрытия внутренних причин.
// Некорректный метод отклоняется до транспорта, а сбой искусственного обмена приводит ровно к одной попытке.
func TestForwardRejectsMalformedRequestAndTransportFailure(t *testing.T) {
	t.Parallel()
	for _, invalid := range []bool{true, false} {
		request := httptest.NewRequest(http.MethodGet, "https://caller.invalid/v2/", nil)
		if invalid {
			request.Method = "invalid method"
		}
		backend := &url.URL{Scheme: "http", Host: "127.0.0.1:5006"}
		calls := 0
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return nil, errors.New("private upstream details")
		})}
		writer := httptest.NewRecorder()
		Forward(writer, request, backend, nil, client, 1<<30)
		want, wantCalls := "upstream unavailable\n", 1
		if invalid {
			want, wantCalls = "upstream request rejected\n", 0
		}
		if writer.Code != http.StatusBadGateway || writer.Body.String() != want || calls != wantCalls {
			t.Fatal("safe forwarding failure changed")
		}
	}
}

// TestForwardKeepsCommittedStatusOnReadFailure pins the existing partial-stream behavior and response closure.
// A body read error after headers must not leak its text, retry the exchange or replace the committed status.
// TestForwardKeepsCommittedStatusOnReadFailure закрепляет прежнее поведение неполного потока и закрытие ответа.
// Ошибка чтения после заголовков не должна раскрывать текст, повторять обмен или заменять отправленный статус.
func TestForwardKeepsCommittedStatusOnReadFailure(t *testing.T) {
	t.Parallel()
	body := &trackedResponseBody{Reader: io.MultiReader(strings.NewReader("partial"), iotest.ErrReader(errors.New("private read details")))}
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusCreated, Body: body}, nil
	})}
	request := httptest.NewRequest(http.MethodGet, "https://caller.invalid/v2/", nil)
	writer := httptest.NewRecorder()
	Forward(writer, request, &url.URL{Scheme: "http", Host: "127.0.0.1:5006"}, nil, client, 1<<30)
	if calls != 1 || writer.Code != http.StatusCreated || writer.Body.String() != "partial" || !body.closed {
		t.Fatal("partial response behavior changed")
	}
}
