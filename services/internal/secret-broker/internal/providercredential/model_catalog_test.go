package providercredential

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// catalogHTTPFunc adapts an in-memory response callback to the catalog HTTP port, keeping API parser
// tests off the network.
// catalogHTTPFunc преобразует callback ответа в памяти в catalog HTTP port, не допуская сетевых запросов
// тестов API parser.
type catalogHTTPFunc func(*http.Request) (*http.Response, error)

// Do forwards the supplied request directly to the synthetic callback, allowing assertions on URL,
// method, body and authorization without a transport.
// Do передаёт запрос прямо synthetic callback для проверки URL, method, body и authorization без
// транспорта.
func (call catalogHTTPFunc) Do(request *http.Request) (*http.Response, error) { return call(request) }

// catalogCapabilityFixture supplies one synthetic reasoning model with an ordered custom effort and one
// plain model, proving validators are not hardcoded to current provider model names.
// catalogCapabilityFixture задаёт одну synthetic reasoning модель с упорядоченным custom effort и одну
// plain модель, показывая отсутствие привязки validator к текущим provider model именам.
func catalogCapabilityFixture() []CatalogModel {
	return []CatalogModel{{ID: "fixture-reasoning", DefaultReasoningEffort: "medium", ReasoningEfforts: []string{"low", "medium", "custom-effort"}}, {ID: "fixture-plain"}}
}

// TestAPIModelCatalogRequiresAccountIntersection exercises complete account subsets, empty/unknown
// lists, authorization and status failures, partial pagination, duplicates, malformed records and
// oversized bodies while asserting the exact synthetic request boundary.
// TestAPIModelCatalogRequiresAccountIntersection проверяет полные account подмножества,
// пустые/неизвестные списки, authorization и status отказы, частичную pagination, повторы, повреждённые
// записи и слишком большой body с точной synthetic request границей.
func TestAPIModelCatalogRequiresAccountIntersection(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
		want       int
		rejected   bool
	}{
		{"subset", `{"object":"list","data":[{"id":"gpt-6-astra","object":"model"},{"id":"unrelated-audio","object":"model"}]}`, 200, 1, false},
		{"empty", `{"object":"list","data":[]}`, 200, 0, false},
		{"unknown", `{"object":"list","data":[{"id":"future-model","object":"model"},{"id":"gpt-5.3-codex-spark","object":"model"}]}`, 200, 0, false},
		{"unauthorized", `{}`, 401, 0, true},
		{"redirect", `{}`, 302, 0, true},
		{"provider_down", `{}`, 503, 0, true},
		{"partial", `{"object":"list","data":[],"has_more":true}`, 200, 0, true},
		{"missing", `{"object":"list"}`, 200, 0, true},
		{"duplicate", `{"object":"list","data":[{"id":"fixture-plain","object":"model"},{"id":"fixture-plain","object":"model"}]}`, 200, 0, true},
		{"wrong_kind", `{"object":"list","data":[{"id":"fixture-plain","object":"unknown"}]}`, 200, 0, true},
		{"syntax", `{"object":"list","data":[]}{}`, 200, 0, true},
		{"oversize", strings.Repeat(" ", maximumModelCatalogBytes+1), 200, 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := catalogHTTPFunc(func(request *http.Request) (*http.Response, error) {
				if request.URL.String() != providerModelsURL || request.Method != http.MethodGet || request.Header.Get("Authorization") != "Bearer synthetic-catalog-key" || request.Body != nil {
					t.Fatal("catalog request boundary changed")
				}
				return &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader(test.body))}, nil
			})
			models, err := readAPIModelCatalog(t.Context(), client, []byte("synthetic-catalog-key"))
			if (err != nil) != test.rejected || !test.rejected && len(models) != test.want || test.rejected && len(models) != 0 {
				t.Fatal("account catalog outcome changed")
			}
		})
	}
}

// TestRemoteCodexCatalogRejectsFallbackAndChangedCapabilities verifies fresh/empty cache intersections
// and rejects stale/future/version/default/identity failures plus missing files, symlinks, hardlinks and
// FIFOs through real local file reads.
// TestRemoteCodexCatalogRejectsFallbackAndChangedCapabilities проверяет пересечение свежего/пустого
// cache и отклоняет stale/future/version/default/identity ошибки, отсутствующие файлы, symlink, hardlink
// и FIFO через реальное локальное чтение файлов.
func TestRemoteCodexCatalogRejectsFallbackAndChangedCapabilities(t *testing.T) {
	started := time.Now().UTC().Add(-time.Second)
	fixture := func() map[string]any {
		return map[string]any{"fetched_at": time.Now().UTC(), "client_version": catalogCodexVersion, "models": []any{
			map[string]any{"slug": "fixture-reasoning", "default_reasoning_level": "medium", "supported_reasoning_levels": []any{map[string]string{"effort": "low"}, map[string]string{"effort": "medium"}, map[string]string{"effort": "custom-effort"}}},
			map[string]any{"slug": "fixture-plain", "supported_reasoning_levels": []any{}},
		}}
	}
	for _, mode := range []string{"fresh", "empty", "stale", "future", "wrong_version", "missing_models", "wrong_default", "duplicate", "missing_file", "symlink", "hardlink", "fifo"} {
		t.Run(mode, func(t *testing.T) {
			home := t.TempDir()
			snapshot := fixture()
			switch mode {
			case "empty":
				snapshot["models"] = []any{}
			case "stale":
				snapshot["fetched_at"] = started.Add(-time.Second)
			case "future":
				snapshot["fetched_at"] = time.Now().UTC().Add(time.Hour)
			case "wrong_version":
				snapshot["client_version"] = "0.0.0"
			case "missing_models":
				delete(snapshot, "models")
			case "wrong_default":
				snapshot["models"].([]any)[0].(map[string]any)["default_reasoning_level"] = "low"
			case "duplicate":
				snapshot["models"] = []any{snapshot["models"].([]any)[0], snapshot["models"].([]any)[0]}
			}
			raw, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(home, "models_cache.json")
			if mode != "missing_file" && mode != "fifo" {
				if err := os.WriteFile(path, raw, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "hardlink" {
				if err := os.Link(path, filepath.Join(home, "linked")); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "symlink" {
				if err := os.Rename(path, filepath.Join(home, "source")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("source", path); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "fifo" {
				createCatalogFIFO(t, path)
			}
			models, err := readRemoteCodexCatalog(home, started, catalogCapabilityFixture())
			if mode == "fresh" || mode == "empty" {
				want := 2
				if mode == "empty" {
					want = 0
				}
				if err != nil || len(models) != want {
					t.Fatal("verified remote catalog rejected")
				}
			} else if err == nil || len(models) != 0 {
				t.Fatal("unverified remote catalog was accepted")
			}
		})
	}
}

// TestCatalogReaderCancellationJoinsBackpressuredStream cancels a reader blocked by an unconsumed events
// channel and requires its done signal within one second without spawning an external process.
// TestCatalogReaderCancellationJoinsBackpressuredStream отменяет reader, заблокированный
// невостребованным events channel, и требует done signal в течение секунды без внешнего процесса.
func TestCatalogReaderCancellationJoinsBackpressuredStream(t *testing.T) {
	stop, done := make(chan struct{}), make(chan struct{})
	events := make(chan streamEvent)
	go readAppServerMessages(strings.NewReader(strings.Repeat("{\"method\":\"notification\"}\n", 100)), events, stop, done)
	close(stop)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("catalog reader did not join")
	}
}

// TestModelCatalogProcessIsolatedCredentialAndCleanup uses the current test executable as the fake
// native process and synthetic HTTP responses to verify route separation, safe results and empty private
// state after each auth mode.
// TestModelCatalogProcessIsolatedCredentialAndCleanup использует текущий test executable как fake native
// process и synthetic HTTP ответы для проверки раздельных маршрутов, безопасных результатов и отсутствия
// private state после каждого auth режима.
func TestModelCatalogProcessIsolatedCredentialAndCleanup(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{CatalogMethodAPIKey, CatalogMethodDeviceCode} {
		t.Run(method, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chmod(root, 0o700); err != nil {
				t.Fatal(err)
			}
			process, err := NewAppServerProcess(binary, root, false)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			process.catalogHTTP = catalogHTTPFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"object":"list","data":[{"id":"gpt-6-astra","object":"model"}]}`))}, nil
			})
			raw := []byte(`{"auth_mode":"apikey","OPENAI_API_KEY":"synthetic-catalog-key"}`)
			if method == CatalogMethodDeviceCode {
				raw = []byte(`{"auth_mode":"chatgpt","tokens":{"access_token":"synthetic-catalog-token","account_id":"synthetic-account","refresh_token":"synthetic-never-forward-refresh"}}`)
			}
			result, err := process.ObserveModelCatalog(t.Context(), raw, method)
			if err != nil || result.Failure != CatalogFailureNone || len(result.Models) != 1 {
				t.Fatalf("catalog process failed: %v, %s", err, result.Failure)
			}
			if method == CatalogMethodAPIKey && calls != 1 || method == CatalogMethodDeviceCode && calls != 0 {
				t.Fatal("authorization mode changed provider path")
			}
			if method == CatalogMethodAPIKey && (result.Source != CatalogRemoteAPI || result.Models[0].ID != "gpt-6-astra" || result.Models[0].IsDefault) || method == CatalogMethodDeviceCode && (result.Source != CatalogRemoteCodex || result.Models[0].ID != "fixture-reasoning" || !result.Models[0].IsDefault) {
				t.Fatal("API and device catalog authority was mixed")
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 {
				t.Fatal("catalog credential directory was retained")
			}
		})
	}
}

// TestStagingModelCatalogAcceptsValidatedProtocolWithoutOptionalCache explicitly enables protocol-only
// fallback on a fake app-server with no cache and requires validated capabilities and private-home
// cleanup; it is not a staging deployment test.
// TestStagingModelCatalogAcceptsValidatedProtocolWithoutOptionalCache явно включает protocol-only
// fallback fake app-server без cache и требует проверенных capabilities с cleanup private home; staging
// deployment не проверяется.
func TestStagingModelCatalogAcceptsValidatedProtocolWithoutOptionalCache(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	process, err := NewAppServerProcess(binary, root, true)
	if err != nil {
		t.Fatal(err)
	}
	auth := []byte(`{"auth_mode":"chatgpt","tokens":{"access_token":"synthetic-catalog-token","account_id":"fixture-cache-missing"}}`)
	result, err := process.ObserveModelCatalog(t.Context(), auth, CatalogMethodDeviceCode)
	if err != nil || result.Failure != CatalogFailureNone || result.Source != CatalogRemoteCodex || len(result.Models) != 1 || result.Models[0].ID != "fixture-reasoning" || !result.Models[0].IsDefault {
		t.Fatalf("validated protocol catalog was rejected: result=%#v err=%v", result, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("protocol catalog retained private state")
	}
}

// TestMain dispatches this test binary's --version and app-server fixture modes before normal unit
// execution, replacing only the external executable/provider while exercising production bounded
// process, stdio and cleanup paths.
// TestMain направляет --version и app-server fixture режимы этого test binary до обычных unit-тестов,
// подменяя только внешний executable/provider и проверяя production ограниченные process, stdio и
// cleanup пути.
func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		_, _ = os.Stdout.WriteString("codex-cli " + catalogCodexVersion + "\n")
		os.Exit(0)
	}
	if len(os.Args) > 1 && os.Args[1] == "app-server" {
		os.Exit(runCatalogProcessFixture())
	}
	os.Exit(m.Run())
}

// runCatalogProcessFixture validates the isolated CLI/proxy environment and serves only synthetic
// initialization, access-token login and model-list traffic over stdio. It can request forbidden
// refresh, hang or write malformed/missing cache fixtures; no provider network is used.
// runCatalogProcessFixture проверяет изолированные CLI/proxy environment и обслуживает только synthetic
// initialization, access-token login и model-list через stdio. Он может запросить запрещённый refresh,
// зависнуть или записать повреждённый/отсутствующий cache fixture; provider сеть не используется.
func runCatalogProcessFixture() int {
	if len(os.Args) != 7 || os.Args[2] != "-c" || os.Args[3] != fileCredentialStoreConfig ||
		os.Args[4] != "--strict-config" || os.Args[5] != "--listen" || os.Args[6] != "stdio://" ||
		os.Getenv("HTTPS_PROXY") != providerEgressProxyURL || os.Getenv("NO_PROXY") != "" {
		return 2
	}
	home := os.Getenv("CODEX_HOME")
	raw, err := os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil && !os.IsNotExist(err) {
		return 3
	}
	defer clear(raw)
	// API observation не должна запускать app-server вообще.
	if len(raw) != 0 {
		return runManagedRenewalFixture(home, raw)
	}
	external, refresh, hang := false, false, false
	diagnosticMode := ""
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			return 4
		}
		if request.Method == "initialized" {
			continue
		}
		var result any = map[string]any{}
		if request.Method == "account/login/start" {
			var params struct {
				Type        string `json:"type"`
				AccessToken string `json:"accessToken"`
				AccountID   string `json:"chatgptAccountId"`
			}
			if len(raw) != 0 || bytes.Contains(request.Params, []byte("refresh")) || json.Unmarshal(request.Params, &params) != nil || params.Type != "chatgptAuthTokens" || params.AccountID == "" {
				return 9
			}
			external, refresh, hang = true, params.AccountID == "fixture-expired", params.AccountID == "fixture-hang"
			diagnosticMode = params.AccountID
			result = map[string]string{"type": "chatgptAuthTokens"}
			if diagnosticMode == "fixture-login" {
				result = map[string]string{"type": "synthetic-private-payload"}
			}
		} else if request.Method == "model/list" {
			if hang {
				time.Sleep(30 * time.Second)
			}
			if refresh {
				if json.NewEncoder(os.Stdout).Encode(map[string]any{"id": 901, "method": "account/chatgptAuthTokens/refresh", "params": map[string]string{"reason": "unauthorized"}}) != nil || !scanner.Scan() {
					return 10
				}
				var rejected struct {
					Error json.RawMessage `json:"error"`
				}
				if json.Unmarshal(scanner.Bytes(), &rejected) != nil || len(rejected.Error) == 0 {
					return 11
				}
				continue
			}
			result = map[string]any{"data": []any{map[string]any{"id": "fixture-reasoning", "model": "fixture-reasoning", "defaultReasoningEffort": "medium", "supportedReasoningEfforts": []any{map[string]string{"reasoningEffort": "medium"}}, "isDefault": true}}, "nextCursor": nil}
			if external {
				cache, _ := json.Marshal(map[string]any{"fetched_at": time.Now().UTC(), "client_version": catalogCodexVersion, "models": []any{map[string]any{"slug": "fixture-reasoning", "default_reasoning_level": "medium", "supported_reasoning_levels": []any{map[string]string{"effort": "medium"}}}}})
				if os.WriteFile(filepath.Join(home, "models_cache.json"), cache, 0o600) != nil {
					return 5
				}
			}
			switch diagnosticMode {
			case "fixture-list-schema":
				result = map[string]string{"unexpected": "synthetic-private-payload"}
			case "fixture-list-identity":
				result = map[string]any{"data": []any{map[string]string{"id": "synthetic-private-payload", "model": "different"}}}
			case "fixture-cache-missing":
				if os.Remove(filepath.Join(home, "models_cache.json")) != nil {
					return 5
				}
			}
		} else if request.Method != "initialize" {
			return 6
		}
		if json.NewEncoder(os.Stdout).Encode(map[string]any{"id": request.ID, "result": result}) != nil {
			return 7
		}
	}
	if scanner.Err() != nil {
		return 8
	}
	return 0
}

// TestModelCatalogProcessRefusesRefreshAndJoinsDeadline makes the fake child request forbidden token
// refresh or hang, then requires authorization refusal or bounded deadline termination with no retained
// private state.
// TestModelCatalogProcessRefusesRefreshAndJoinsDeadline заставляет fake child запросить запрещённый
// token refresh или зависнуть, затем требует authorization отказа либо ограниченного завершения по
// дедлайну без оставшегося private state.
func TestModelCatalogProcessRefusesRefreshAndJoinsDeadline(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, account := range []string{"fixture-expired", "fixture-hang"} {
		t.Run(account, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chmod(root, 0o700); err != nil {
				t.Fatal(err)
			}
			process, err := NewAppServerProcess(binary, root, false)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(map[string]any{"auth_mode": "chatgpt", "tokens": map[string]string{"access_token": "synthetic-catalog-token", "account_id": account, "refresh_token": "synthetic-never-forward-refresh"}})
			if err != nil {
				t.Fatal(err)
			}
			defer clear(raw)
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			started := time.Now()
			result, err := process.ObserveModelCatalog(ctx, raw, CatalogMethodDeviceCode)
			if account == "fixture-expired" {
				if err != nil || result.Failure != CatalogFailureAuthorization || len(result.Models) != 0 {
					t.Fatal("external token refresh was not rejected")
				}
			} else if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 5*time.Second {
				t.Fatal("catalog process did not join after deadline")
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 {
				t.Fatal("catalog process retained credential state")
			}
		})
	}
}

// TestCatalogAuthenticationRejectsMismatchedAndUnconfiguredMaterial rejects missing, short and
// mixed-mode auth JSON for API observation before any process or HTTP call.
// TestCatalogAuthenticationRejectsMismatchedAndUnconfiguredMaterial отклоняет отсутствующий, короткий и
// смешанный auth JSON для API наблюдения до любых process и HTTP вызовов.
func TestCatalogAuthenticationRejectsMismatchedAndUnconfiguredMaterial(t *testing.T) {
	for _, raw := range []string{`{}`, `{"auth_mode":"apikey","OPENAI_API_KEY":"short"}`, `{"auth_mode":"chatgpt","OPENAI_API_KEY":"synthetic-catalog-key"}`, `{"auth_mode":"apikey","OPENAI_API_KEY":"synthetic-catalog-key","tokens":{}}`} {
		if _, err := catalogAuthentication([]byte(raw), CatalogMethodAPIKey); !errors.Is(err, errModelCatalogAuthorization) {
			t.Fatal("unconfigured credential accepted")
		}
	}
}
