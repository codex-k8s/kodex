package integration

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestContext7WorkingPathAndReadiness exercises both tools over local SDK JSON and SSE servers, checks typed arguments and receipts, and proves readiness lists tools without invoking them.
// TestContext7WorkingPathAndReadiness проверяет оба инструмента на локальных SDK-серверах JSON и SSE, типизированные аргументы и квитанции, а также отсутствие вызова инструментов при проверке готовности.
func TestContext7WorkingPathAndReadiness(t *testing.T) {
	for _, jsonResponse := range []bool{true, false} {
		t.Run(map[bool]string{true: "JSON", false: "SSE"}[jsonResponse], func(t *testing.T) {
			var calls atomic.Int32
			adapter := context7Fixture(t, jsonResponse, func(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				calls.Add(1)
				var arguments map[string]string
				if err := json.Unmarshal(request.Params.Arguments, &arguments); err != nil {
					t.Error(err)
				}
				field := "libraryName"
				if request.Params.Name == "query-docs" {
					field = "libraryId"
				}
				if len(arguments) != 2 || arguments[field] == "" || arguments["query"] != "usage" {
					t.Errorf("unexpected typed arguments: %v", arguments)
				}
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "bounded documentation"}}}, nil
			})
			credential := testCredential(t, adapter, "fixture-context7-key")
			for _, operation := range []string{"context7.library.resolve", "context7.docs.query"} {
				input := map[string]any{"library_name": "Go SDK", "query": "usage"}
				if operation == "context7.docs.query" {
					input = map[string]any{"library_id": "/modelcontextprotocol/go-sdk", "query": "usage"}
				}
				request := context7Request(t, adapter, operation, input, credential)
				result, err := adapter.Execute(t.Context(), request)
				if err != nil {
					t.Fatal(err)
				}
				if result.Summary != `{"text":"bounded documentation"}` || result.Receipt.EffectKey != request.EffectKey || result.Receipt.InputDigest != request.InputDigest || !strings.HasPrefix(result.Receipt.ProviderEffectRef, "context7:") {
					t.Fatalf("invalid receipt: %+v", result)
				}
				request.Input = nil
				if _, err := adapter.Test(t.Context(), request); err != nil {
					t.Fatal(err)
				}
			}
			if calls.Load() != 2 {
				t.Fatalf("readiness called a provider tool: %d", calls.Load())
			}
		})
	}
}

// TestContext7RejectsProviderFailureAndUnsafeContent rejects tool failures, an exact credential echo, oversized text, images and blank output using a local MCP fixture without leaking private error text.
// TestContext7RejectsProviderFailureAndUnsafeContent отклоняет ошибки инструмента, точное повторение учётных данных, длинный текст, изображения и пустой вывод на локальной MCP-оснастке без утечки закрытого текста ошибки.
func TestContext7RejectsProviderFailureAndUnsafeContent(t *testing.T) {
	for name, response := range map[string]*mcp.CallToolResult{
		"isError":    {IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "provider private error"}}},
		"credential": {Content: []mcp.Content{&mcp.TextContent{Text: "fixture-context7-key"}}},
		"oversized":  {Content: []mcp.Content{&mcp.TextContent{Text: strings.Repeat("x", context7TextBytes+1)}}},
		"non-text":   {Content: []mcp.Content{&mcp.ImageContent{Data: []byte{1}, MIMEType: "image/png"}}},
		"empty":      {Content: []mcp.Content{&mcp.TextContent{Text: " "}}},
	} {
		t.Run(name, func(t *testing.T) {
			adapter := context7Fixture(t, true, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) { return response, nil })
			credential := testCredential(t, adapter, "fixture-context7-key")
			_, err := adapter.Execute(t.Context(), context7Request(t, adapter, "context7.library.resolve", map[string]any{"library_name": "sdk", "query": "usage"}, credential))
			if err == nil || strings.Contains(err.Error(), "fixture-context7-key") || strings.Contains(err.Error(), "provider private error") {
				t.Fatalf("unsafe error: %v", err)
			}
		})
	}
}

// TestContext7DeadlineAndProtocolFailure verifies cancellation of a stalled local tool within the caller budget and rejection of a malformed MCP readiness response.
// TestContext7DeadlineAndProtocolFailure проверяет отмену зависшего локального инструмента в пределах срока вызывающего кода и отклонение некорректного MCP-ответа готовности.
func TestContext7DeadlineAndProtocolFailure(t *testing.T) {
	stop := make(chan struct{})
	adapter := context7Fixture(t, true, func(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		select {
		case <-ctx.Done():
		case <-stop:
		}
		return nil, ctx.Err()
	})
	t.Cleanup(func() { close(stop) })
	credential := testCredential(t, adapter, "fixture-context7-key")
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := adapter.Execute(ctx, context7Request(t, adapter, "context7.library.resolve", map[string]any{"library_name": "sdk", "query": "usage"}, credential)); err == nil {
		t.Fatal("deadline accepted")
	}
	if time.Since(started) > time.Second {
		t.Fatal("deadline budget exceeded")
	}
	adapter.context7HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"invalid":"protocol"}`))}, nil
	})}
	ctx, cancelProtocol := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancelProtocol()
	if _, err := adapter.Test(ctx, Request{Configuration: map[string]any{"base_url": context7Origin}, Credential: credential, DefinitionPackage: context7Request(t, adapter, "context7.library.resolve", map[string]any{"library_name": "sdk", "query": "usage"}, credential).DefinitionPackage, DefinitionKey: "context7", DefinitionVersion: "1.0.0", DefinitionDigest: adapter.definitions["context7"].Digest}); err == nil {
		t.Fatal("invalid protocol accepted")
	}
}

// TestContext7TransportClosedBoundary inspects exact TLS/proxy settings and rejects alternate URLs, invalid or oversized requests and known/streamed response overflow before unsafe forwarding.
// TestContext7TransportClosedBoundary проверяет точные настройки TLS и прокси, отклоняет посторонние URL, некорректные или длинные запросы и объявленное либо потоковое переполнение ответа до небезопасной передачи.
func TestContext7TransportClosedBoundary(t *testing.T) {
	const integrationProxy = "http://egress-gateway-openapi.kodex-system.svc.cluster.local:8083"
	client, err := newContext7HTTPClient(Config{ProxyURL: "http://egress-gateway.kodex-system.svc.cluster.local:8080", OpenAPIProxyURL: integrationProxy, Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	base := client.Transport.(*http.Transport)
	if base.TLSClientConfig.MinVersion != tls.VersionTLS13 || base.TLSClientConfig.ServerName != "mcp.context7.com" || base.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("invalid TLS boundary")
	}
	request, _ := http.NewRequest(http.MethodPost, context7Endpoint, nil)
	proxy, _ := base.Proxy(request)
	if proxy.String() != integrationProxy {
		t.Fatal("invalid CONNECT route")
	}
	for _, invalidProxy := range []string{"", "http://127.0.0.1:8083", "http://egress-gateway.kodex-system.svc.cluster.local:8080", integrationProxy + "/", integrationProxy + "?override=1"} {
		if _, err := newContext7HTTPClient(Config{ProxyURL: "http://egress-gateway.kodex-system.svc.cluster.local:8080", OpenAPIProxyURL: invalidProxy, Timeout: time.Second}); err == nil {
			t.Fatal("alternate integration proxy accepted")
		}
	}
	var calls int
	transport := context7Transport{credential: "fixture-context7-key", base: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, nil })}
	for _, address := range []string{"https://127.0.0.1/mcp", "http://mcp.context7.com/mcp", "https://mcp.context7.com/other", context7Endpoint + "?key=invalid", "https://user@mcp.context7.com/mcp"} {
		request, _ := http.NewRequest(http.MethodPost, address, nil)
		if _, err := transport.RoundTrip(request); err == nil {
			t.Fatalf("URL accepted: %s", address)
		}
	}
	if calls != 0 {
		t.Fatal("invalid target reached transport")
	}
	for _, size := range []int{context7RequestBytes + 1, 100} {
		body := strings.Repeat("x", size)
		request, _ := http.NewRequest(http.MethodPost, context7Endpoint, strings.NewReader(body))
		if _, err := transport.RoundTrip(request); err == nil {
			t.Fatal("invalid request body accepted")
		}
	}
	for _, responseLength := range []int64{-1, context7ResponseBytes + 1} {
		transport.base = roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, ContentLength: responseLength, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", context7ResponseBytes+1)))}, nil
		})
		request, _ := http.NewRequest(http.MethodPost, context7Endpoint, strings.NewReader(`{"method":"tools/list"}`))
		response, err := transport.RoundTrip(request)
		if err == nil {
			_, err = io.ReadAll(response.Body)
			_ = response.Body.Close()
		}
		if err == nil {
			t.Fatal("oversized response accepted")
		}
	}
}

// TestContext7CatalogSchema accepts the required two-string schema and rejects incomplete or malformed required-field lists without claiming full JSON Schema coverage.
// TestContext7CatalogSchema принимает обязательную схему двух строк и отклоняет неполные либо некорректные списки обязательных полей, не заявляя полное покрытие JSON Schema.
func TestContext7CatalogSchema(t *testing.T) {
	valid := map[string]any{"type": "object", "properties": map[string]any{"libraryName": map[string]any{"type": "string"}, "query": map[string]any{"type": "string"}}, "required": []any{"libraryName", "query"}}
	if !context7Schema(valid, "libraryName") {
		t.Fatal("valid schema rejected")
	}
	valid["required"] = []any{"libraryName"}
	if context7Schema(valid, "libraryName") {
		t.Fatal("incomplete schema accepted")
	}
	valid["required"] = []any{map[string]any{}, map[string]any{}}
	if context7Schema(valid, "libraryName") {
		t.Fatal("malformed schema accepted")
	}
}

// TestContext7ReadinessRejectsMissingToolAndCredentials removes one required local tool and then the credential, checking that readiness/execution fail without calling a tool.
// TestContext7ReadinessRejectsMissingToolAndCredentials удаляет обязательный локальный инструмент, затем учётные данные и проверяет отказ готовности и выполнения без вызова инструмента.
func TestContext7ReadinessRejectsMissingToolAndCredentials(t *testing.T) {
	var calls atomic.Int32
	adapter := context7Fixture(t, true, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls.Add(1)
		return nil, nil
	}, func(server *mcp.Server) { server.RemoveTools("query-docs") })
	credential := testCredential(t, adapter, "fixture-context7-key")
	request := context7Request(t, adapter, "context7.library.resolve", map[string]any{"library_name": "sdk", "query": "usage"}, credential)
	if _, err := adapter.Test(t.Context(), request); err == nil {
		t.Fatal("missing required tool accepted")
	}
	if _, err := adapter.Execute(t.Context(), request); err == nil {
		t.Fatal("working call skipped catalog probe")
	}
	request.Credential = nil
	if _, err := adapter.Test(t.Context(), request); err == nil {
		t.Fatal("missing credential accepted")
	}
	if calls.Load() != 0 {
		t.Fatal("unready adapter called a tool")
	}
}

// TestContext7RejectsRedirectAndHTTPFailure injects redirect, authentication, rate-limit and server responses, requiring one request and sanitized errors with no private provider text.
// TestContext7RejectsRedirectAndHTTPFailure подставляет ответы перенаправления, авторизации, ограничения частоты и сервера, требуя один запрос и безопасные ошибки без закрытого текста провайдера.
func TestContext7RejectsRedirectAndHTTPFailure(t *testing.T) {
	for _, status := range []int{http.StatusFound, http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			adapter := testAdapter(t)
			credential := testCredential(t, adapter, "fixture-context7-key")
			var calls int
			adapter.context7HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: status, Header: http.Header{"Location": []string{"https://127.0.0.1/private"}}, Body: io.NopCloser(strings.NewReader("fixture-context7-key private failure"))}, nil
			})}
			request := context7Request(t, adapter, "context7.library.resolve", map[string]any{"library_name": "sdk", "query": "usage"}, credential)
			_, err := adapter.Execute(t.Context(), request)
			if err == nil || calls != 1 || strings.Contains(err.Error(), "fixture-context7-key") || strings.Contains(err.Error(), "private") {
				t.Fatalf("unsafe failure or redirect: %v, calls=%d", err, calls)
			}
		})
	}
}

// context7Fixture starts a local SDK MCP server with two typed tools and optional catalog mutations, rerouting the fixed authenticated provider request only inside the fixture.
// context7Fixture запускает локальный SDK MCP-сервер с двумя типизированными инструментами и необязательными изменениями каталога, перенаправляя закреплённый авторизованный запрос провайдера только внутри оснастки.
func context7Fixture(t *testing.T, jsonResponse bool, handler mcp.ToolHandler, mutations ...func(*mcp.Server)) *Adapter {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "context7-fixture", Version: "1.0.0"}, nil)
	for _, name := range []string{"resolve-library-id", "query-docs"} {
		field := "libraryName"
		if name == "query-docs" {
			field = "libraryId"
		}
		server.AddTool(&mcp.Tool{Name: name, InputSchema: map[string]any{"type": "object", "properties": map[string]any{field: map[string]any{"type": "string"}, "query": map[string]any{"type": "string"}}, "required": []string{field, "query"}}}, handler)
	}
	for _, mutate := range mutations {
		mutate(server)
	}
	fixture := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: jsonResponse, PropagateRequestCancellation: true}))
	t.Cleanup(fixture.Close)
	fixtureURL, _ := url.Parse(fixture.URL)
	adapter := testAdapter(t)
	adapter.context7HTTPClient = &http.Client{Timeout: 2 * time.Second, Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != context7Endpoint || request.Header.Get("CONTEXT7_API_KEY") != "fixture-context7-key" || request.Header.Get("Authorization") != "" {
			t.Error("invalid authenticated request boundary")
		}
		clone := request.Clone(request.Context())
		clone.URL.Scheme, clone.URL.Host, clone.Host = fixtureURL.Scheme, fixtureURL.Host, fixtureURL.Host
		return http.DefaultTransport.RoundTrip(clone)
	})}
	return adapter
}

// context7Request builds a synthetic invocation from the shipped Context7 capability, canonical input and scope digests with fixed fixture grant references; it does not issue production authority.
// context7Request создаёт синтетический вызов из поставляемой возможности Context7, канонического входа и хешей области с тестовыми ссылками гранта; производственных полномочий не выдаёт.
func context7Request(t *testing.T, adapter *Adapter, operation string, input map[string]any, credential *CredentialRevision) Request {
	t.Helper()
	definition := adapter.definitions["context7"]
	capability, ok := definition.Capability(operation)
	if !ok {
		t.Fatal("context7 capability missing")
	}
	configuration := map[string]string{"base_url": context7Origin}
	scope, err := capability.ResourceScopeValues(configuration)
	if err != nil {
		t.Fatal(err)
	}
	encodedScope, _ := json.Marshal(scope)
	scopeDigest := sha256.Sum256(encodedScope)
	encodedInput, _ := json.Marshal(input)
	canonicalInput, err := capability.ValidateInput(encodedInput)
	if err != nil {
		t.Fatal(err)
	}
	inputDigest := sha256.Sum256(canonicalInput)
	definitionPackage, _ := json.Marshal(definition)
	return Request{DefinitionPackage: definitionPackage, DefinitionKey: "context7", DefinitionVersion: definition.Metadata.Version, DefinitionDigest: definition.Digest, ConnectionRef: "int_test", GrantRef: "igr_fixture01", GrantVersion: 1, CapabilityKey: operation, Operation: operation, Risk: "READ", ApprovalPolicy: "NONE", ResourceKind: capability.ResourceScope.Kind, ResourceScope: scope, ResourceScopeDigest: hex.EncodeToString(scopeDigest[:]), EffectKey: "eff_0123456789abcdef0123456789abcdef", InputDigest: hex.EncodeToString(inputDigest[:]), Configuration: map[string]any{"base_url": context7Origin}, Input: input, Credential: credential}
}

// testContext7CatalogOperation executes one advertised operation against a local MCP server and checks a single tool call, projected text and matching input/effect receipt.
// testContext7CatalogOperation выполняет одну объявленную операцию на локальном MCP-сервере и проверяет единственный вызов инструмента, текстовую проекцию и соответствие квитанции входу и эффекту.
func testContext7CatalogOperation(t *testing.T, operation string, input map[string]any) {
	t.Helper()
	var calls atomic.Int32
	adapter := context7Fixture(t, true, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls.Add(1)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "bounded documentation"}}}, nil
	})
	credential := testCredential(t, adapter, "fixture-context7-key")
	request := context7Request(t, adapter, operation, input, credential)
	result, err := adapter.Execute(t.Context(), request)
	if err != nil || calls.Load() != 1 || result.Summary != `{"text":"bounded documentation"}` || result.Receipt.EffectKey != request.EffectKey || result.Receipt.InputDigest != request.InputDigest {
		t.Fatalf("advertised Context7 operation failed: %v", err)
	}
}
