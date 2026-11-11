package opencode

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	// nativeLiteLLMImage pins the official1.104.2 image, independently of a mutable release tag.
	// nativeLiteLLMImage закрепляет официальный image1.104.2 независимо от mutable release tag.
	nativeLiteLLMImage = "docker.io/litellm/litellm@sha256:3d88bd757134a00e8c00237b85a021ab1f6d54abb9c1df750f411dbe7256a500"
	// nativeRedisImage pins the isolated response-cache backend; no shared Redis is used or flushed.
	// nativeRedisImage закрепляет isolated response-cache backend; общий Redis не используется и не очищается.
	nativeRedisImage = "docker.io/library/redis@sha256:82403a0812a51faf6779f8d0dbe25ea85279ec7d49d24a3c78a65e9d8a87fa5f"
)

// nativeDocker confines qualification resources to a verified local Unix Docker endpoint and one random ownership label.
// nativeDocker ограничивает qualification resources проверенным local Unix Docker endpoint и одной random ownership label.
type nativeDocker struct {
	t               *testing.T
	endpoint, scope string
	sensitive       []string
}

// command runs only explicit synthetic harness operations; timeout is bounded and no agent credentials are passed as environment.
// command выполняет лишь explicit synthetic harness operations с bounded timeout без agent credentials в environment.
func (d nativeDocker) command(arguments ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", append([]string{"--host", d.endpoint}, arguments...)...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}
	raw, err := cmd.CombinedOutput()
	for _, secret := range d.sensitive {
		if len(secret) >= 8 {
			raw = []byte(strings.ReplaceAll(string(raw), secret, "[REDACTED]"))
		}
	}
	if len(raw) > 4096 {
		raw = append(append(raw[:1024:1024], []byte("\n[bounded synthetic diagnostics]\n")...), raw[len(raw)-3072:]...)
	}
	return strings.TrimSpace(string(raw)), err
}

// removeContainer verifies its exact random ownership label before deleting a disposable container.
// removeContainer проверяет exact random ownership label до удаления disposable container.
func (d nativeDocker) removeContainer(name string) {
	raw, err := d.command("inspect", "--format", `{{index .Config.Labels "kodex.native-test"}}`, name)
	if err == nil && raw == d.scope {
		_, _ = d.command("rm", "--force", name)
	}
}

// start registers reconciliation-safe cleanup before container creation; failures never retry a lost create.
// start регистрирует reconciliation-safe cleanup до container create; потерянный create не повторяется.
func (d nativeDocker) start(name string, arguments ...string) {
	d.t.Helper()
	d.t.Cleanup(func() { d.removeContainer(name) })
	args := []string{"run", "--detach", "--pull=never", "--name", name, "--label", "kodex.native-test=" + d.scope, "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--pids-limit", "256"}
	if _, err := d.command(append(args, arguments...)...); err != nil {
		d.t.Fatal("start isolated native cache qualification container")
	}
}

// TestNativeLiteLLMRedisBypass proves real response-cache read/write bypass through actual OpenCode, LiteLLM and Redis with a controlled model only.
// TestNativeLiteLLMRedisBypass доказывает настоящий response-cache read/write bypass через OpenCode, LiteLLM и Redis с заменой лишь модели.
func TestNativeLiteLLMRedisBypass(t *testing.T) {
	if os.Getenv("KODEX_TEST_LITELLM_REDIS") != "1" {
		t.Skip("set KODEX_TEST_LITELLM_REDIS=1 for isolated Docker qualification")
	}
	binary := qualifiedNativeBinary(t)
	endpoint := os.Getenv("DOCKER_HOST")
	if endpoint == "" {
		raw, err := exec.Command("docker", "context", "inspect", "--format", `{{.Endpoints.docker.Host}}`).Output()
		if err != nil {
			t.Fatal("local Docker context unavailable")
		}
		endpoint = strings.TrimSpace(string(raw))
	}
	if !strings.HasPrefix(endpoint, "unix:///") || strings.ContainsAny(endpoint, "\r\n") {
		t.Fatal("cache qualification requires a local Unix Docker endpoint")
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	scope := "kodex-native-2108-" + hex.EncodeToString(random[:])
	docker := nativeDocker{t: t, endpoint: endpoint, scope: scope}
	if _, err := docker.command("network", "create", "--internal", "--label", "kodex.native-test="+scope, scope); err != nil {
		t.Fatal("create isolated cache qualification network")
	}
	t.Cleanup(func() {
		raw, err := docker.command("network", "inspect", "--format", `{{index .Labels "kodex.native-test"}}`, scope)
		if err == nil && raw == scope {
			_, _ = docker.command("network", "rm", scope)
		}
	})
	fixture := &nativeFixture{cacheExpectation: &nativeCacheWireExpectation{mechanism: "VLLM_CACHE_SALT", namespace: strings.Repeat("c", 64)}, identityKey: strings.Repeat("d", 64), identityRevision: strings.Repeat("b", 64), nativeIdentities: map[string]bool{}}
	model := httptest.NewUnstartedServer(http.HandlerFunc(fixture.model))
	directory := t.TempDir()
	listener, err := net.Listen("unix", filepath.Join(directory, "upstream.sock"))
	model.Listener = listener
	if err != nil {
		t.Fatal("bind private synthetic model socket")
	}
	model.Start()
	t.Cleanup(model.Close)
	redisName := scope + "-redis"
	proxyName := scope + "-proxy"
	docker.start(redisName, "--network", scope, "--network-alias", "redis", "--memory", "128m", "--cpus", "0.5", "--user", "999:999", nativeRedisImage, "redis-server", "--save", "", "--appendonly", "no")
	var proxyKey [32]byte
	if _, err := rand.Read(proxyKey[:]); err != nil {
		t.Fatal(err)
	}
	credential := "sk-" + hex.EncodeToString(proxyKey[:])
	docker.sensitive = []string{credential}
	configPath := filepath.Join(directory, "proxy.yaml")
	config := fmt.Sprintf("model_list:\n  - model_name: tiny\n    litellm_params:\n      model: hosted_vllm/tiny\n      api_base: %s/v1\n      api_key: synthetic-not-a-credential\nrouter_settings:\n  num_retries: 0\n  timeout: 15\nlitellm_settings:\n  telemetry: false\n  cache: true\n  cache_params:\n    type: redis\n    host: redis\n    port: 6379\n    ttl: 300\n", "http://127.0.0.1:4001")
	config += "general_settings:\n  master_key: " + credential + "\n"
	if os.WriteFile(configPath, []byte(config), 0600) != nil {
		t.Fatal("write private synthetic proxy config")
	}
	// UID/GID владельца сохраняют private mode 0600 без CAP_DAC_OVERRIDE и chmod для остальных пользователей.
	docker.start(proxyName, "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "--network", scope, "--memory", "1536m", "--cpus", "2", "--read-only", "--tmpfs", "/tmp:rw,noexec,nosuid,size=128m",
		"--env", "LITELLM_LOCAL_MODEL_COST_MAP=True", "--env", "LITELLM_LOG=INFO", "--env", "PYTHONUNBUFFERED=1", "--mount", "type=bind,src="+directory+",dst=/harness", "--mount", "type=bind,src="+configPath+",dst=/config/proxy.yaml,readonly", "--entrypoint", "python", nativeLiteLLMImage, "-c", nativeLiteLLMUnixRelay, "--config", "/config/proxy.yaml", "--host", "127.0.0.1", "--port", "4000", "--num_workers", "1")
	origin := "http://litellm.example.invalid"
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(directory, "proxy.sock"))
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: nativeCacheAuthTransport{base: transport, credential: credential}, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("synthetic proxy redirect rejected") }}

	defer client.CloseIdleConnections()
	ready := false
	for deadline := time.Now().Add(45 * time.Second); time.Now().Before(deadline); {
		response, err := client.Get(origin + "/health/liveliness")
		if err == nil {
			io.Copy(io.Discard, response.Body)
			response.Body.Close()
			if response.StatusCode == 200 {
				ready = true
				break
			}
		}
		state, _ := docker.command("inspect", "--format", `{{.State.Status}}`, proxyName)
		if state == "exited" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		state, _ := docker.command("inspect", "--format", `{{.State.Status}}/{{.State.ExitCode}}/{{.State.OOMKilled}}`, proxyName)
		logs, _ := docker.command("logs", "--tail", "200", proxyName)
		t.Fatalf("synthetic LiteLLM is not ready: %s / %s", state, logs)
	}
	var once sync.Once
	var wireMu sync.Mutex
	requests, compactions := 0, 0
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
		var body map[string]any
		if err != nil || json.Unmarshal(raw, &body) != nil {
			w.WriteHeader(400)
			return
		}
		cache, _ := body["cache"].(map[string]any)
		if len(cache) != 2 || cache["no-cache"] != true || cache["no-store"] != true || body["disable_fallbacks"] != true {
			t.Error("actual native request lost proxy cache/fallback controls")
			w.WriteHeader(400)
			return
		}
		wireMu.Lock()
		requests++
		if strings.Contains(string(raw), "SYNTHETIC_NATIVE_COMPACTION") {
			compactions++
		}
		wireMu.Unlock()
		once.Do(func() { qualifyLiteLLMCacheReadWrite(t, client, origin, body, fixture) })
		response, err := nativeProxyRequest(client, origin, raw)
		if err != nil {
			w.WriteHeader(502)
			return
		}
		defer response.Body.Close()
		w.Header().Set("Content-Type", response.Header.Get("Content-Type"))
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	}))
	defer front.Close()
	nativeConfig := nativeToolConfiguration(t, fixture)
	nativeConfig["provider"].(map[string]any)["synthetic"].(map[string]any)["options"].(map[string]string)["baseURL"] = front.URL + "/v1"
	home := t.TempDir()
	if os.Chmod(home, 0700) != nil {
		t.Fatal("prepare private native HOME")
	}
	plugin, err := InstallManagedToolPlugin(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(home, "config", "opencode"), 0700) })
	nativeConfig["plugin"] = []string{plugin}
	spec, _ := json.Marshal(map[string]any{"provider_id": "synthetic", "model_id": "tiny", "base_url": front.URL + "/v1", "api": "CHAT_COMPLETIONS", "route_kind": "LITELLM", "inference_cache": map[string]any{"mechanism": "VLLM_CACHE_SALT", "namespace": strings.Repeat("c", 64), "no_reuse": false}})
	server := startConfiguredNativeServerEnvironment(t, binary, home, nativeConfig, []string{"KODEX_MODEL_TRANSPORT=" + string(spec), "KODEX_MCP_PROXY_TOKEN=" + fixture.identityKey, "KODEX_NATIVE_REVISION=" + fixture.identityRevision})
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	session, err := server.client.CreateSession(ctx, "Synthetic real LiteLLM cache qualification")
	if err != nil {
		t.Fatal(err)
	}
	result, err := server.client.Execute(ctx, session.ID, Prompt{MessageID: "msg_C1234567890123456789012345", Model: Model{ProviderID: "synthetic", ModelID: "tiny"}, Parts: []TextPart{{Type: "text", Text: "Complete the synthetic managed counter."}}}, func(name string) bool { return name == "kodex_counter" }, func(string, string) error { return nil })
	if err != nil || result.FinalText != "SYNTHETIC_FINAL_COUNTER_12" || result.Compactions < 1 {
		t.Fatal("native loop through real LiteLLM failed", err)
	}
	fixture.mu.Lock()
	tools, violations := fixture.tools, len(fixture.violations)
	fixture.mu.Unlock()
	wireMu.Lock()
	defer wireMu.Unlock()
	if tools != 12 || violations != 0 || compactions < 1 {
		t.Fatal("real proxy path lost tools, compaction or route controls")
	}
	t.Logf("OpenCode1.19.5 -> LiteLLM1.104.2 -> synthetic model: modelRequests=%d compactionRequests=%d managedEffects=%d; seeded Redis read/write bypass and upstream salt forwarding qualified", requests, compactions, tools)
}

// nativeProxyRequest makes one local synthetic model request without retries, redirects, host proxy inheritance or real credentials.
// nativeProxyRequest выполняет один local synthetic model request без retry, redirect, host proxy inheritance и real credentials.
func nativeProxyRequest(client *http.Client, origin string, raw []byte) (*http.Response, error) {
	request, err := http.NewRequest(http.MethodPost, origin+"/v1/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	return client.Do(request)
}

// qualifyLiteLLMCacheReadWrite proves cache effectiveness first, then checks seeded-read bypass and absence of a stored bypass result by upstream counters.
// qualifyLiteLLMCacheReadWrite сначала доказывает работу кеша, затем проверяет seeded-read bypass и отсутствие stored bypass result по upstream counters.
func qualifyLiteLLMCacheReadWrite(t *testing.T, client *http.Client, origin string, body map[string]any, fixture *nativeFixture) {
	t.Helper()
	count := func() int { fixture.mu.Lock(); defer fixture.mu.Unlock(); return fixture.modelCalls }
	send := func(value map[string]any) {
		raw, _ := json.Marshal(value)
		response, err := nativeProxyRequest(client, origin, raw)
		if err != nil {
			t.Error("synthetic cache probe transport failed")
			return
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			t.Errorf("synthetic cache probe status=%d", response.StatusCode)
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 2<<20))
	}
	clone := func(value map[string]any) map[string]any {
		raw, _ := json.Marshal(value)
		var copy map[string]any
		_ = json.Unmarshal(raw, &copy)
		return copy
	}
	baseline := clone(body)
	delete(baseline, "cache")
	send(baseline)
	hit := false
	for i := 0; i < 10; i++ {
		time.Sleep(100 * time.Millisecond)
		before := count()
		send(baseline)
		if count() == before {
			hit = true
			break
		}
	}
	if !hit {
		t.Error("isolated Redis baseline never served a cached response")
		return
	}
	for i := 0; i < 2; i++ {
		before := count()
		send(body)
		if count() != before+1 {
			t.Error("seeded response cache served native bypass request")
		}
	}
	fresh := clone(baseline)
	fresh["temperature"] = 0.125
	bypass := clone(fresh)
	bypass["cache"] = map[string]bool{"no-cache": true, "no-store": true}
	before := count()
	send(bypass)
	if count() != before+1 {
		t.Error("fresh bypass did not reach synthetic upstream")
	}
	before = count()
	send(fresh)
	if count() != before+1 {
		t.Error("bypass response was retained in Redis")
	}
	hit = false
	for i := 0; i < 10; i++ {
		time.Sleep(100 * time.Millisecond)
		before = count()
		send(fresh)
		if count() == before {
			hit = true
			break
		}
	}
	if !hit {
		t.Error("post-bypass Redis baseline failed; no-store proof is inconclusive")
	}
}

// nativeLiteLLMUnixRelay bridges only the private synthetic socket and starts the real proxy; it supports rootless Docker without exposing a host listener.
// nativeLiteLLMUnixRelay связывает только private synthetic socket и запускает настоящий proxy, поддерживая rootless Docker без host listener.
const nativeLiteLLMUnixRelay = `import os, select, socket, socketserver, sys
class Relay(socketserver.BaseRequestHandler):
    def handle(self):
        is_model = self.server.address_family == socket.AF_INET
        family = socket.AF_UNIX if is_model else socket.AF_INET
        target_address = "/harness/upstream.sock" if is_model else ("127.0.0.1", 4000)
        with socket.socket(family, socket.SOCK_STREAM) as upstream:
            try:
                upstream.connect(target_address)
            except OSError:
                return
            peers = [self.request, upstream]
            while peers:
                readable, _, _ = select.select(peers, [], [], 30)
                if not readable:
                    return
                for source in readable:
                    target = upstream if source is self.request else self.request
                    data = source.recv(65536)
                    if not data:
                        return
                    target.sendall(data)
if os.fork() == 0:
    socketserver.ThreadingTCPServer.allow_reuse_address = True
    with socketserver.ThreadingTCPServer(("127.0.0.1", 4001), Relay) as server:
        server.serve_forever()
if os.fork() == 0:
    os.umask(0o077)
    with socketserver.ThreadingUnixStreamServer("/harness/proxy.sock", Relay) as server:
        server.serve_forever()
os.execvp("litellm", ["litellm"] + sys.argv[1:])
`

// nativeCacheAuthTransport supplies an in-memory test-only credential solely to the owned Unix-socket proxy, never argv or a remote host.
// nativeCacheAuthTransport передаёт in-memory test credential лишь owned Unix-socket proxy, никогда argv или remote host.
type nativeCacheAuthTransport struct {
	base       http.RoundTripper
	credential string
}

// RoundTrip rejects foreign destinations and adds authentication only to the cloned owned request.
// RoundTrip отклоняет foreign destinations и добавляет authentication лишь cloned owned request.
func (a nativeCacheAuthTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Host != "litellm.example.invalid" {
		return nil, fmt.Errorf("synthetic proxy destination rejected")
	}
	request = request.Clone(request.Context())
	request.Header.Set("Authorization", "Bearer "+a.credential)
	return a.base.RoundTrip(request)
}
