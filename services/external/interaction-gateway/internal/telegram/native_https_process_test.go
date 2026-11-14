package telegram

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

// nativeTelegramCertificate creates a disposable CA and exact api.telegram.org TLS leaf in memory; system trust and production keys remain untouched.
// nativeTelegramCertificate создаёт disposable CA и точный api.telegram.org TLS leaf в памяти; system trust и production keys не меняются.
func nativeTelegramCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal("generate synthetic TLS key")
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Disposable Telegram Test CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal("generate synthetic TLS CA")
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"api.telegram.org"}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter,
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal("generate synthetic TLS leaf")
	}
	private, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal("encode synthetic TLS key")
	}
	certificate, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: private}))
	roots := x509.NewCertPool()
	if err != nil || !roots.AppendCertsFromPEM(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})) {
		t.Fatal("prepare synthetic TLS trust")
	}
	return certificate, roots
}

// TestNativeTelegramHTTPSUsesCONNECTAndDefaultTransport runs health and fenced send through the production transport, a separate actual CONNECT gateway and a TLS provider; only test DNS/dial mapping and an ephemeral CA are substituted.
// TestNativeTelegramHTTPSUsesCONNECTAndDefaultTransport выполняет health и fenced send через production transport, отдельный настоящий CONNECT gateway и TLS provider; заменяются лишь test DNS/dial mapping и ephemeral CA.
func TestNativeTelegramHTTPSUsesCONNECTAndDefaultTransport(t *testing.T) {
	binary := os.Getenv("KODEX_TEST_NATIVE_GATEWAY_BINARY")
	if binary == "" {
		t.Skip("run make test-telegram-native-egress for the cross-process TLS qualification")
	}
	info, err := os.Lstat(binary)
	if err != nil || !info.Mode().IsRegular() || !filepath.IsAbs(binary) {
		t.Fatal("exact native gateway test binary required")
	}
	certificate, roots := nativeTelegramCertificate(t)
	var calls, sends, connections, proxyConnections, traces atomic.Int32
	var mode atomic.Value
	mode.Store("normal")
	api := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		if request.Host != "api.telegram.org" || request.TLS == nil || request.TLS.Version < tls.VersionTLS13 || request.TLS.ServerName != "api.telegram.org" ||
			request.Method != http.MethodPost || request.Header.Get("Authorization") != "" || !strings.HasPrefix(request.URL.Path, "/bot"+fixtureToken+"/") {
			t.Error("native Telegram request escaped exact TLS and credential boundary")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		method := strings.TrimPrefix(request.URL.Path, "/bot"+fixtureToken+"/")
		if mode.Load() == "redirect" {
			w.Header().Set("Location", "https://other.example.invalid/forbidden")
			w.WriteHeader(http.StatusFound)
			return
		}
		var result any
		switch method {
		case "getMe":
			result = map[string]any{"id": 123456, "is_bot": true}
		case "getChat":
			result = map[string]any{"id": -1001234, "type": "supergroup", "is_forum": true, "permissions": map[string]any{"can_send_messages": true}}
		case "getChatMember":
			result = map[string]any{"status": "administrator", "user": map[string]any{"id": 123456, "is_bot": true}}
		case "sendMessage":
			sends.Add(1)
			var input map[string]any
			if json.NewDecoder(request.Body).Decode(&input) != nil || input["chat_id"] != float64(-1001234) || input["message_thread_id"] != float64(77) || input["protect_content"] != true {
				t.Error("native send changed exact target or safe template")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if mode.Load() == "drop-send" {
				connection, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error("native provider could not simulate lost response")
					return
				}
				_ = connection.Close()
				return
			}
			if mode.Load() == "rate-limit" {
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = io.WriteString(w, `{"ok":false,"error_code":429,"parameters":{"retry_after":30}}`)
				return
			}
			message := map[string]any{"message_id": 123, "message_thread_id": 77, "chat": map[string]any{"id": -1001234, "type": "supergroup"},
				"from": map[string]any{"id": 123456, "is_bot": true}, "text": input["text"]}
			if mode.Load() == "invalid-send" {
				message["message_thread_id"] = nil
			}
			result = message
		default:
			t.Error("native Telegram transport used an undeclared method")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
	}))
	api.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate}}
	api.Config.ErrorLog = log.New(io.Discard, "", 0)
	api.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	api.StartTLS()
	defer api.Close()
	// The child must join in Cleanup after testing cancels t.Context; its independent deadline still bounds a broken fixture.
	// Child обязан завершиться в Cleanup после отмены t.Context testing; независимый deadline по-прежнему ограничивает неисправную fixture.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	command := exec.CommandContext(ctx, binary, "-test.run=^TestNativeGatewayProcessFixture$", "-test.timeout=55s")
	command.Dir = "../../../egress-gateway/internal/gateway"
	command.Env = []string{"KODEX_TEST_NATIVE_GATEWAY_PROCESS=synthetic-only"}
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal("prepare native gateway input")
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal("prepare native gateway readback")
	}
	var diagnostics bytes.Buffer
	command.Stderr = &diagnostics
	if err := command.Start(); err != nil {
		t.Fatal("start native gateway fixture")
	}
	reader := bufio.NewReader(stdout)
	t.Cleanup(func() {
		defer cancel()
		_ = stdin.Close()
		output, _ := io.ReadAll(io.LimitReader(reader, 65536))
		joined := command.Wait()
		detail := string(output) + diagnostics.String()
		if strings.Contains(detail, fixtureToken) || strings.Contains(detail, "/bot") {
			t.Error("native diagnostics exposed a token-bearing URL")
			detail = "credential-bearing diagnostic redacted"
		}
		if joined != nil {
			t.Errorf("native gateway fixture did not join cleanly: %v; %s", joined, detail)
		}
	})
	if json.NewEncoder(stdin).Encode(map[string]string{"upstream": api.Listener.Addr().String(), "profile": "telegram"}) != nil {
		t.Fatal("configure native gateway fixture")
	}
	line, err := reader.ReadBytes('\n')
	var listener struct{ Listen, Digest string }
	if err != nil || json.Unmarshal(line, &listener) != nil || len(listener.Digest) != 64 {
		t.Fatal("native gateway readback is invalid")
	}
	address, err := net.ResolveTCPAddr("tcp", listener.Listen)
	if err != nil || !address.IP.IsLoopback() {
		t.Fatal("native gateway listener is not local")
	}
	adapter, claim := telegramAdapterBudgetFixture(t, 30*time.Second)
	base := adapter.defaultTransport().(*http.Transport).Clone()
	base.TLSClientConfig.RootCAs = roots
	base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "egress-gateway.kodex-system.svc.cluster.local:8086" {
			t.Error("native transport bypassed the fixed Telegram proxy")
			return nil, context.Canceled
		}
		proxyConnections.Add(1)
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, listener.Listen)
	}
	adapter.newTransport = func() http.RoundTripper { return base.Clone() }
	configuration := map[string]any{}
	for key, value := range claim.GetTelegram().GetConfiguration() {
		configuration[key] = value
	}
	public, err := structpb.NewStruct(configuration)
	if err != nil {
		t.Fatal("prepare native health claim")
	}
	health := &cp.IntegrationConnectionTestClaim{DefinitionKey: claim.GetDefinitionKey(), DefinitionVersion: claim.GetDefinitionVersion(), DefinitionDigest: claim.GetDefinitionDigest(),
		DefinitionPackage: claim.GetDefinitionPackage(), CredentialRevision: claim.GetCredentialDescriptor(), PublicConfiguration: public}
	traced := httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{WroteHeaders: func() { traces.Add(1) }})
	if _, err := adapter.TestConnection(traced, health); err != nil || calls.Load() != 3 {
		t.Fatal("native health did not reach exact TLS provider")
	}
	beginCount := 0
	begin := func(context.Context, *cp.BeginInteractionDeliveryRequest) (*cp.BeginInteractionDeliveryResponse, error) {
		beginCount++
		return &cp.BeginInteractionDeliveryResponse{DeliveryRef: claim.GetDeliveryRef(), InputDigest: claim.GetTelegram().GetInputDigest(), Allowed: true}, nil
	}
	if receipt, err := adapter.Deliver(traced, claim, begin); err != nil || receipt.GetMessageId() != 123 || sends.Load() != 1 || beginCount != 1 || calls.Load() != 7 || traces.Load() != 0 {
		t.Fatal("native fenced send lost scope, TLS or trace boundary")
	}
	for _, failure := range []string{"wrong-ca", "wrong-sni"} {
		t.Run(failure, func(t *testing.T) {
			transport := base.Clone()
			if failure == "wrong-ca" {
				transport.TLSClientConfig.RootCAs = x509.NewCertPool()
			} else {
				transport.TLSClientConfig.ServerName = "other.example.invalid"
			}
			adapter.newTransport = func() http.RoundTripper { return transport.Clone() }
			before, connected, begun := calls.Load(), connections.Load(), beginCount
			_, err := adapter.Deliver(traced, claim, begin)
			if err == nil || !ConfirmedNoEffect(err) || calls.Load() != before || beginCount != begun || strings.Contains(err.Error(), fixtureToken) || strings.Contains(err.Error(), "/bot") {
				t.Fatal("untrusted peer received credential or leaked raw URL")
			}
			if failure == "wrong-sni" && connections.Load() != connected {
				t.Fatal("wrong SNI crossed gateway dial boundary")
			}
		})
	}
	adapter.newTransport = func() http.RoundTripper { return base.Clone() }
	mode.Store("redirect")
	before, begun, connected := calls.Load(), beginCount, proxyConnections.Load()
	if _, err := adapter.Deliver(traced, claim, begin); err == nil || !ConfirmedNoEffect(err) || calls.Load() != before+1 || beginCount != begun || proxyConnections.Load() != connected+1 {
		t.Fatal("native redirect was followed or reached Begin")
	}
	for _, failure := range []string{"drop-send", "invalid-send", "rate-limit"} {
		mode.Store(failure)
		before, sent, begun := calls.Load(), sends.Load(), beginCount
		_, err := adapter.Deliver(traced, claim, begin)
		if err == nil || calls.Load() != before+4 || sends.Load() != sent+1 || beginCount != begun+1 || traces.Load() != 0 || strings.Contains(err.Error(), fixtureToken) || strings.Contains(err.Error(), "/bot") {
			t.Fatal("native failure replayed send or leaked token path")
		}
		if failure == "rate-limit" {
			if !ConfirmedNoEffect(err) {
				t.Fatal("confirmed native rate limit became an unknown send")
			}
		} else if ConfirmedNoEffect(err) {
			t.Fatal("possible native send was made retryable")
		}
	}
}
