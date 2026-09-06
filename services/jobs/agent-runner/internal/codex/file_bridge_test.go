package codex

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/readiness"
)

func TestFileBridgeNonRootProtocol(t *testing.T) {
	if os.Getenv("KODEX_FILE_BRIDGE_TEST") == "1" {
		runFileBridgeFixture(t)
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "bwrap", "--unshare-user", "--uid", "10002", "--gid", "29000", "--tmpfs", "/", "--ro-bind", "/usr", "/usr", "--ro-bind", "/lib", "/lib", "--ro-bind", "/lib64", "/lib64", "--ro-bind", executable, "/bridge-test", "--bind", workspace, "/workspace", "--dir", "/run/kodex/provider", "--chdir", "/workspace", "/bridge-test", "-test.run=^TestFileBridgeNonRootProtocol$")
	command.Env = []string{"KODEX_FILE_BRIDGE_TEST=1", "PATH=/usr/bin:/bin", "TMPDIR=/workspace"}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("non-root file bridge: %v\n%s", err, output)
	}
}

func runFileBridgeFixture(t *testing.T) {
	if os.Geteuid() != 10002 || os.Getegid() != 29000 {
		t.Fatal("fixture identity is invalid")
	}
	const size = 33<<20 + 7
	chunk := bytes.Repeat([]byte("f"), 64<<10)
	hash := sha256.New()
	for remaining := size; remaining > 0; {
		n := min(remaining, len(chunk))
		_, _ = hash.Write(chunk[:n])
		remaining -= n
	}
	digest := "sha256:" + hex.EncodeToString(hash.Sum(nil))
	input := model.Input{Mode: runtimecontract.RunnerModeTurn, ProjectRef: "prj_fixture01", LeaseRef: "lease_fixture01", LeaseFence: "fixture-fence", LeaseGeneration: 1, RuntimeRevisionDigest: strings.Repeat("d", 64), ExecutionBindingDigest: strings.Repeat("e", 64), FileCatalog: &runtimecontract.RuntimeFileCatalog{Ref: "vfc_fixture01", Digest: strings.Repeat("a", 64), Total: 1, Purposes: []string{"PROJECT"}}}
	var downloads atomic.Int32
	var expectedPeer []byte
	ticket := strings.Repeat("c", 64)
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || r.TLS.Version != tls.VersionTLS13 || len(r.TLS.PeerCertificates) != 1 || !bytes.Equal(r.TLS.PeerCertificates[0].Raw, expectedPeer) || r.Header.Get("Authorization") != "Bearer "+ticket {
			t.Error("missing authenticated controller peer")
			w.WriteHeader(403)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/mcp") {
			var rpc struct {
				ID     string `json:"id"`
				Method string `json:"method"`
			}
			if json.NewDecoder(r.Body).Decode(&rpc) != nil {
				w.WriteHeader(400)
				return
			}
			if rpc.Method == "notifications/initialized" {
				w.WriteHeader(202)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			result := any(map[string]any{"protocolVersion": "2025-06-18"})
			if rpc.Method == "tools/list" {
				result = map[string]any{"tools": []any{map[string]any{"name": "search_files", "description": "Fixture file search", "inputSchema": map[string]any{"type": "object"}}}}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": rpc.ID, "result": result})
			return
		}
		downloads.Add(1)
		if r.URL.Path != "/v1/executions/lease_fixture01/artifacts/art_fixture01" || r.Header.Get("X-Kodex-Callback-Method") != "artifact" || r.Header.Get("X-Kodex-Execution-Binding-Digest") != input.ExecutionBindingDigest || r.Header.Get("Cookie") != "" || r.URL.Query().Get("digest") != digest {
			t.Error("file authority changed")
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(size))
		w.Header().Set("X-Kodex-Artifact-Digest", digest)
		for remaining := size; remaining > 0; {
			n := min(remaining, len(chunk))
			if _, err := w.Write(chunk[:n]); err != nil {
				return
			}
			remaining -= n
		}
	}))
	upstream.TLS = &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAnyClientCert}
	upstream.StartTLS()
	expectedPeer = upstream.Certificate().Raw
	defer upstream.Close()
	root := "/workspace"
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: upstream.Certificate().Raw})
	key, err := x509.MarshalPKCS8PrivateKey(upstream.TLS.Certificates[0].PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string][]byte{"ca.pem": cert, "client.pem": cert, "client.key": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), "ticket": []byte(ticket)} {
		if err := os.WriteFile(filepath.Join(root, name), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	input.CallbackURL = upstream.URL
	input.ExecutionTicketFile = filepath.Join(root, "ticket")
	input.CallbackTLS = model.TLSBinding{ServerName: upstream.Certificate().DNSNames[0], CAFile: filepath.Join(root, "ca.pem"), CertificateFile: filepath.Join(root, "client.pem"), PrivateKeyFile: filepath.Join(root, "client.key")}
	proxy, err := readiness.StartMCPProxy(t.Context(), input, ticket, []string{"search_files"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Second)
		defer cancel()
		if err := proxy.Close(ctx); err != nil {
			t.Error(err)
		}
	}()
	bridge, err := startProviderMCPBridge(t.Context(), proxy.SocketPath(), proxy.LocalBearerToken(), input)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	base, _ := url.Parse(bridge.URL())
	base.Path = "/v1/executions/lease_fixture01/artifacts/art_fixture01"
	base.RawQuery = url.Values{"purpose": {"PROJECT"}, "entry_ref": {"vfe_fixture01"}, "revision": {"1"}, "digest": {digest}}.Encode()
	client := &http.Client{Timeout: 10 * time.Second}
	request, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, base.String(), nil)
	request.Header.Set("Authorization", "Bearer "+proxy.LocalBearerToken())
	request.Header.Set("Cookie", "caller=hidden")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	hash.Reset()
	n, copyErr := io.Copy(hash, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != 200 || copyErr != nil || n != size || "sha256:"+hex.EncodeToString(hash.Sum(nil)) != digest || downloads.Load() != 1 {
		t.Fatalf("full bridge transfer failed status=%d size=%d error=%v", response.StatusCode, n, copyErr)
	}
	for _, mode := range []string{"token", "lease", "selector"} {
		request, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, base.String(), nil)
		request.Header.Set("Authorization", "Bearer "+proxy.LocalBearerToken())
		switch mode {
		case "token":
			request.Header.Set("Authorization", "Bearer foreign")
		case "lease":
			request.URL.Path = strings.ReplaceAll(request.URL.Path, "lease_fixture01", "lease_foreign01")
		case "selector":
			request.URL.RawQuery += "&project_ref=prj_foreign01"
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if response.StatusCode != 404 || downloads.Load() != 1 {
			t.Fatal("invalid provider request reached authority")
		}
	}
}
