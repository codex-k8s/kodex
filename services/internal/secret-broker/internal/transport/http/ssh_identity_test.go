package httptransport

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	sshrepository "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/sshtarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/service/sshidentity"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/integration/sshtarget"
	"golang.org/x/crypto/ssh"
)

// uploadFixture substitutes only transactional owner/store ports; TLS, key parsing, transport and domain workflow are real, while native PostgreSQL/109 remains a separate qualification.
// uploadFixture заменяет лишь transactional owner/store ports; TLS, key parsing, transport и domain workflow реальные, а native PostgreSQL/109 квалифицируется отдельно.
type uploadFixture struct {
	mu                                           sync.Mutex
	handler                                      *SSHIdentityUpload
	client                                       *http.Client
	origin, token, permitDigest, auth            string
	material                                     []byte
	snapshot                                     value.SSHIdentityUploadSnapshot
	resolves, writes, reads, claims, completions int
	eligible, closed                             bool
	failure                                      error
	logs                                         bytes.Buffer
}

// ResolveSSHIdentityUpload refuses terminal or abandoned attempts; only a committed UNKNOWN authorizes observation of the original immutable intent.
// ResolveSSHIdentityUpload отклоняет terminal либо abandoned attempts; лишь committed UNKNOWN разрешает observation original immutable intent.
func (f *uploadFixture) ResolveSSHIdentityUpload(_ context.Context, ref, permit string) (value.SSHIdentityUploadSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolves++
	if ref != f.snapshot.SetupRef || permit != f.permitDigest || f.closed || f.claims > 0 && !f.eligible {
		return value.SSHIdentityUploadSnapshot{}, sshrepository.ErrIdentityRejected
	}
	s := f.snapshot
	if f.claims > 0 {
		s.Mode = value.SSHIdentityUploadObserve
		s.AuthenticationSHA256 = f.auth
	}
	return s, nil
}

// ClaimSSHIdentityUpload records the sole write or a receipt-backed observation before returning one exact short admission.
// ClaimSSHIdentityUpload фиксирует sole write либо receipt-backed observation до возврата exact short admission.
func (f *uploadFixture) ClaimSSHIdentityUpload(_ context.Context, in sshrepository.IdentityUploadClaim) (value.SSHIdentityUploadAdmission, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if in.SetupRef != f.snapshot.SetupRef || in.PermitSHA256 != f.permitDigest || in.SnapshotDigest != f.snapshot.Digest || f.closed || f.claims > 0 && !f.eligible {
		return value.SSHIdentityUploadAdmission{}, sshrepository.ErrIdentityRejected
	}
	mode := value.SSHIdentityUploadWrite
	if f.claims > 0 {
		mode = value.SSHIdentityUploadObserve
	}
	f.claims++
	f.auth = in.Key.AuthenticationSHA256
	p, ok := f.snapshot.Preparation.Provision(in.Key)
	if !ok {
		return value.SSHIdentityUploadAdmission{}, sshrepository.ErrIdentityRejected
	}
	return value.SSHIdentityUploadAdmission{SetupRef: in.SetupRef, SnapshotDigest: in.SnapshotDigest, AttemptRef: "ssa_http_fixture", WorkloadInstance: in.WorkloadInstance, Generation: int64(f.claims), Fence: "fence_http_fixture", Mode: mode, Provision: p, ExpiresAt: time.Now().Add(20 * time.Second)}, nil
}

// CompleteSSHIdentityUpload classifies explicit evidence; an HTTP error or elapsed lease never becomes a synthetic receipt.
// CompleteSSHIdentityUpload классифицирует explicit evidence; HTTP error либо elapsed lease не превращается в synthetic receipt.
func (f *uploadFixture) CompleteSSHIdentityUpload(_ context.Context, in sshrepository.IdentityUploadCompletion) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completions++
	f.eligible = in.Unknown
	f.closed = !in.Unknown
	return nil
}

// CreateSSHIdentity counts one admitted material effect and returns either exact original pins or the configured classified failure without retry.
// CreateSSHIdentity считает один admitted material effect и возвращает exact original pins либо classified failure без retry.
func (f *uploadFixture) CreateSSHIdentity(_ context.Context, p value.SSHIdentityProvision, _ []byte) (value.SSHIdentityLocator, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes++
	if f.failure != nil {
		return value.SSHIdentityLocator{}, f.failure
	}
	return uploadLocator(p), nil
}

// ObserveSSHIdentity observes the immutable original tuple without creating another object.
// ObserveSSHIdentity наблюдает immutable original tuple без создания другого объекта.
func (f *uploadFixture) ObserveSSHIdentity(_ context.Context, p value.SSHIdentityProvision) (value.SSHIdentityLocator, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	return uploadLocator(p), nil
}

// ReadSSHIdentity forbids setup from using the unrelated runtime credential reader.
// ReadSSHIdentity запрещает setup использовать unrelated runtime credential reader.
func (*uploadFixture) ReadSSHIdentity(context.Context, value.SSHIdentityLocator) ([]byte, error) {
	return nil, sshrepository.ErrIdentityRejected
}

// DeleteSSHIdentity forbids cleanup because the upload permit grants no deletion.
// DeleteSSHIdentity запрещает cleanup, поскольку upload permit не разрешает deletion.
func (*uploadFixture) DeleteSSHIdentity(context.Context, value.SSHIdentityLocator) error {
	return sshrepository.ErrIdentityRejected
}

// counts reads the joined synthetic effect/read boundary under its mutex, without reflecting material in assertion output.
// counts читает joined synthetic effect/read boundary под mutex без material в assertion output.
func (f *uploadFixture) counts() (int, int, int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writes, f.reads, f.resolves, f.completions
}

// uploadLocator constructs exact private synthetic coordinates, never public metadata or another source of material-read authority.
// uploadLocator создаёт exact private synthetic coordinates, а не public metadata либо новый источник material-read authority.
func uploadLocator(p value.SSHIdentityProvision) value.SSHIdentityLocator {
	return value.SSHIdentityLocator{Purpose: p.Purpose, IdentityRevisionRef: p.IdentityRevisionRef, TargetRef: p.TargetRef, Revision: p.Revision, Namespace: p.Namespace, SecretName: managedssh.IdentitySecretName(p.IdentityRevisionRef, p.Revision), SecretUID: "fixture-uid", SecretResourceVersion: "fixture-rv", ContentSHA256: p.ContentSHA256, AuthenticationSHA256: p.AuthenticationSHA256}
}

// newUploadFixture composes actual Ed25519 parsing/workflow under verified TLS1.3/SNI with an ephemeral loopback server and exact test dialer; no live credential or kubeconfig is read.
// newUploadFixture связывает actual Ed25519 parsing/workflow под verified TLS1.3/SNI с ephemeral loopback server и exact test dialer; live credential и kubeconfig не читаются.
func newUploadFixture(t *testing.T, literal ...bool) *uploadFixture {
	t.Helper()
	f := &uploadFixture{}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal("generate synthetic key")
	}
	block, err := ssh.MarshalPrivateKey(private, "")
	if err != nil {
		t.Fatal("encode synthetic key")
	}
	f.material = pem.EncodeToMemory(block)
	t.Cleanup(func() { clear(private); clear(f.material) })
	permit := bytes.Repeat([]byte{3}, 32)
	f.token = "Bearer " + base64.RawURLEncoding.EncodeToString(permit)
	pd := sha256.Sum256(permit)
	clear(permit)
	f.permitDigest = hex.EncodeToString(pd[:])
	content := sha256.Sum256(f.material)
	f.snapshot = value.SSHIdentityUploadSnapshot{SetupRef: "ssu_http_fixture", Digest: strings.Repeat("a", 64), Mode: value.SSHIdentityUploadWrite, ContentBytes: uint32(len(f.material)), ExpiresAt: time.Now().Add(time.Minute), Preparation: value.SSHIdentityPreparation{IdentityRevisionRef: "bki_http_fixture", TargetRef: "ssh_http_fixture", Revision: 1, Namespace: "synthetic-ssh", ContentSHA256: hex.EncodeToString(content[:])}}
	service, err := sshidentity.New(f, sshtarget.KeyPreparer{}, f, "pod_http_fixture", "synthetic-ssh")
	if err != nil {
		t.Fatal("compose domain service")
	}
	server := httptest.NewUnstartedServer(nil)
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	port := strconv.Itoa(server.Listener.Addr().(*net.TCPAddr).Port)
	name := "example.com"
	if len(literal) > 0 && literal[0] {
		name = "127.0.0.1"
	}
	f.origin = "https://" + name + ":" + port
	f.handler, err = NewSSHIdentityUpload(service, managedssh.IdentityUploadEndpoint{Origin: f.origin, TLSServerName: name}, "https://control.example.invalid", slog.New(slog.NewTextHandler(&f.logs, nil)))
	if err != nil {
		t.Fatal("compose HTTPS handler")
	}
	server.Config.Handler = f.handler
	server.Config.ReadHeaderTimeout = time.Second
	server.Config.ReadTimeout = 2 * time.Second
	server.Config.WriteTimeout = 5 * time.Second
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	server.StartTLS()
	t.Cleanup(server.Close)
	transport := server.Client().Transport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	transport.TLSClientConfig.ServerName = name
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != name+":"+port {
			return nil, errors.New("synthetic destination rejected")
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	f.client = &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	t.Cleanup(transport.CloseIdleConnections)
	return f
}

// request sends one exact route and reads only bounded metadata; failed assertions never include a key, permit or raw dependency error.
// request отправляет один exact route и читает лишь bounded metadata; failed assertions не содержат key, permit либо raw dependency error.
func (f *uploadFixture) request(t *testing.T, action string, body []byte, change func(*http.Request)) (int, string) {
	t.Helper()
	r, err := http.NewRequest(http.MethodPost, f.origin+sshUploadPath+"ssu_http_fixture/"+action, bytes.NewReader(body))
	if err != nil {
		t.Fatal("construct request")
	}
	r.Header.Set("Authorization", f.token)
	r.Header.Set("Origin", "https://control.example.invalid")
	if action == "material" {
		r.Header.Set("Content-Type", "application/octet-stream")
	}
	if change != nil {
		change(r)
	}
	response, err := f.client.Do(r)
	if err != nil {
		t.Fatal("synthetic TLS request failed")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 2048))
	if err != nil {
		t.Fatal("read metadata")
	}
	if response.TLS == nil || response.TLS.Version != tls.VersionTLS13 || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("TLS or response privacy boundary changed")
	}
	return response.StatusCode, string(raw)
}

// TestSSHUploadTLSWorkflow checks actual browser preflight, one parsed upload and terminal replay, then explicit UNKNOWN observation without a second material effect.
// TestSSHUploadTLSWorkflow проверяет actual browser preflight, parsed upload и terminal replay, затем explicit UNKNOWN observation без второго material effect.
func TestSSHUploadTLSWorkflow(t *testing.T) {
	f := newUploadFixture(t)
	status, _ := f.request(t, "material", nil, func(r *http.Request) {
		r.Method = http.MethodOptions
		r.Header.Del("Authorization")
		r.Header.Set("Access-Control-Request-Method", "POST")
		r.Header.Set("Access-Control-Request-Headers", "authorization, content-type")
	})
	_, _, resolves, _ := f.counts()
	if status != http.StatusNoContent || resolves != 0 {
		t.Fatal("preflight spent owner authority")
	}
	status, body := f.request(t, "material", f.material, nil)
	writes, _, _, completed := f.counts()
	if status != http.StatusOK || !strings.Contains(body, "EVIDENCE_RECORDED") || writes != 1 || completed != 1 {
		t.Fatal("one admitted upload did not complete")
	}
	status, _ = f.request(t, "material", f.material, nil)
	writes, _, _, _ = f.counts()
	if status == http.StatusOK || writes != 1 {
		t.Fatal("replay repeated material write")
	}
	g := newUploadFixture(t)
	g.failure = sshrepository.ErrIdentityUnknown
	status, body = g.request(t, "material", g.material, nil)
	if status != http.StatusAccepted || !strings.Contains(body, "UNKNOWN") {
		t.Fatal("ambiguous result lost classification")
	}
	status, _ = g.request(t, "observe", nil, nil)
	writes, reads, _, _ := g.counts()
	if status != http.StatusOK || writes != 1 || reads != 1 {
		t.Fatal("observation repeated materialization")
	}
}

// TestSSHUploadRejectsForeignTransportBeforeOwner verifies exact origin/Host, method, aliases, permit and bounded raw/chunked bodies before any owner read.
// TestSSHUploadRejectsForeignTransportBeforeOwner проверяет exact origin/Host, method, aliases, permit и bounded raw/chunked bodies до owner read.
func TestSSHUploadRejectsForeignTransportBeforeOwner(t *testing.T) {
	for _, change := range []func(*http.Request){func(r *http.Request) { r.Header.Set("Origin", "https://foreign.example.invalid") }, func(r *http.Request) { r.Header.Add("Origin", "https://control.example.invalid") }, func(r *http.Request) { r.Header.Set("Origin", "") }, func(r *http.Request) { r.Host = "foreign.example.invalid" }, func(r *http.Request) { r.URL.RawQuery = "permit=forbidden" }, func(r *http.Request) { r.Method = http.MethodGet }, func(r *http.Request) { r.Header.Set("Authorization", "Bearer invalid") }, func(r *http.Request) { r.Header.Add("Authorization", r.Header.Get("Authorization")) }, func(r *http.Request) { r.Header.Set("Cookie", "ambient=forbidden") }, func(r *http.Request) { r.Header.Set("Content-Encoding", "gzip") },
		func(r *http.Request) { r.Header.Set("Content-Encoding", "") }, func(r *http.Request) { r.Header.Set("Content-Type", "application/json") }} {
		f := newUploadFixture(t)
		status, _ := f.request(t, "material", f.material, change)
		writes, _, resolves, _ := f.counts()
		if status < 400 || resolves != 0 || writes != 0 {
			t.Fatal("foreign transport reached owner")
		}
	}
	for _, chunked := range []bool{false, true} {
		f := newUploadFixture(t)
		status, _ := f.request(t, "material", bytes.Repeat([]byte{'x'}, 16385), func(r *http.Request) {
			if chunked {
				r.ContentLength = -1
			}
		})
		_, _, resolves, _ := f.counts()
		if status < 400 || resolves != 0 {
			t.Fatal("oversized request reached owner")
		}
	}
	f := newUploadFixture(t)
	r := httptest.NewRequest(http.MethodPost, "http://example.com/", nil)
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, r)
	if recorder.Code != http.StatusForbidden {
		t.Fatal("plaintext admitted")
	}
}

// TestSSHUploadRejectedSuccessAndRedaction preserves terminal ACK rejection and keeps all private dependency diagnostics out of the body and boundary log.
// TestSSHUploadRejectedSuccessAndRedaction сохраняет terminal ACK rejection и исключает private dependency diagnostics из body и boundary log.
func TestSSHUploadRejectedSuccessAndRedaction(t *testing.T) {
	f := newUploadFixture(t)
	f.failure = &sshrepository.IdentityAcknowledgementRejection{ResponseSHA256: strings.Repeat("b", 64), ObservedUID: "fixture-observed-uid"}
	status, body := f.request(t, "material", f.material, nil)
	if status != http.StatusConflict || !strings.Contains(body, "ACK_REJECTED") {
		t.Fatal("damaged success misclassified")
	}
	status, _ = f.request(t, "observe", nil, nil)
	writes, reads, _, _ := f.counts()
	if status == http.StatusOK || writes != 1 || reads != 0 {
		t.Fatal("rejected success gained recovery credit")
	}
	g := newUploadFixture(t)
	g.failure = errors.New("synthetic-secret-must-not-appear")
	status, body = g.request(t, "material", g.material, nil)
	if status != http.StatusServiceUnavailable || strings.Contains(body, "synthetic-secret") || strings.Contains(g.logs.String(), "synthetic-secret") || strings.Contains(g.logs.String(), g.token) || strings.Contains(g.logs.String(), "PRIVATE KEY") {
		t.Fatal("private diagnostics escaped")
	}
}

// TestSSHUploadIPOriginAndCertificateFailure preserves exact IP-host TLS semantics without SNI fallback and proves a wrong server name is rejected before the HTTP owner path.
// TestSSHUploadIPOriginAndCertificateFailure сохраняет exact IP-host TLS semantics без SNI fallback и доказывает отказ wrong server name до HTTP owner path.
func TestSSHUploadIPOriginAndCertificateFailure(t *testing.T) {
	f := newUploadFixture(t, true)
	status, _ := f.request(t, "material", f.material, nil)
	if status != http.StatusOK {
		t.Fatal("literal IP endpoint incorrectly required a DNS SNI")
	}
	g := newUploadFixture(t)
	transport := g.client.Transport.(*http.Transport).Clone()
	transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	transport.TLSClientConfig.ServerName = "foreign.example.invalid"
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	r, _ := http.NewRequest(http.MethodPost, g.origin+sshUploadPath+"ssu_http_fixture/material", bytes.NewReader(g.material))
	r.Header.Set("Authorization", g.token)
	r.Header.Set("Content-Type", "application/octet-stream")
	response, err := client.Do(r)
	if response != nil {
		response.Body.Close()
	}
	_, _, resolves, _ := g.counts()
	if err == nil || resolves != 0 {
		t.Fatal("wrong TLS name reached owner")
	}
}

// TestSSHUploadUntrustedCAStopsBeforeHTTP proves that the native client does not substitute an insecure or plaintext path when certificate trust is absent.
// TestSSHUploadUntrustedCAStopsBeforeHTTP доказывает отсутствие insecure либо plaintext path при недоверенном сертификате native client.
func TestSSHUploadUntrustedCAStopsBeforeHTTP(t *testing.T) {
	f := newUploadFixture(t)
	transport := f.client.Transport.(*http.Transport).Clone()
	transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	transport.TLSClientConfig.RootCAs = x509.NewCertPool()
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	r, _ := http.NewRequest(http.MethodPost, f.origin+sshUploadPath+"ssu_http_fixture/material", bytes.NewReader(f.material))
	r.Header.Set("Authorization", f.token)
	r.Header.Set("Content-Type", "application/octet-stream")
	response, err := client.Do(r)
	if response != nil {
		response.Body.Close()
	}
	_, _, resolves, _ := f.counts()
	if err == nil || resolves != 0 {
		t.Fatal("untrusted CA reached owner")
	}
}

// TestSSHUploadPreflightDuplicatesAndUnsupportedDeadlineRefuseBeforeOwner rejects ambiguous CORS method headers and an HTTP wrapper that cannot enforce the body deadline.
// TestSSHUploadPreflightDuplicatesAndUnsupportedDeadlineRefuseBeforeOwner отклоняет ambiguous CORS method headers и HTTP wrapper без body deadline enforcement до owner.
func TestSSHUploadPreflightDuplicatesAndUnsupportedDeadlineRefuseBeforeOwner(t *testing.T) {
	f := newUploadFixture(t)
	status, _ := f.request(t, "material", nil, func(r *http.Request) {
		r.Method = http.MethodOptions
		r.Header.Del("Authorization")
		r.Header.Add("Access-Control-Request-Method", "POST")
		r.Header.Add("Access-Control-Request-Method", "DELETE")
		r.Header.Set("Access-Control-Request-Headers", "authorization")
	})
	_, _, resolves, _ := f.counts()
	if status != http.StatusForbidden || resolves != 0 {
		t.Fatal("ambiguous preflight admitted")
	}
	r := httptest.NewRequest(http.MethodPost, f.origin+sshUploadPath+"ssu_http_fixture/material", bytes.NewReader(f.material))
	r.URL.Scheme = ""
	r.URL.Host = ""
	r.TLS = &tls.ConnectionState{Version: tls.VersionTLS13, ServerName: "example.com"}
	r.Header.Set("Authorization", f.token)
	r.Header.Set("Content-Type", "application/octet-stream")
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, r)
	_, _, resolves, _ = f.counts()
	if recorder.Code != http.StatusServiceUnavailable || resolves != 0 {
		t.Fatalf("unsupported deadline expected status503/no owner read, actual status%d owner reads%d", recorder.Code, resolves)
	}
}
