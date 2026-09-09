package proxycontract

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	middlewareapi "github.com/oauth2-proxy/oauth2-proxy/v7/pkg/apis/middleware"
	"github.com/oauth2-proxy/oauth2-proxy/v7/pkg/apis/options"
	sessionsapi "github.com/oauth2-proxy/oauth2-proxy/v7/pkg/apis/sessions"
	"github.com/oauth2-proxy/oauth2-proxy/v7/pkg/logger"
	"github.com/oauth2-proxy/oauth2-proxy/v7/pkg/middleware"
	cookiestore "github.com/oauth2-proxy/oauth2-proxy/v7/pkg/sessions/cookie"
	redisstore "github.com/oauth2-proxy/oauth2-proxy/v7/pkg/sessions/redis"
)

// Оснастка запускает только свой контейнер с loopback port и одноразовыми данными.
func TestProxySessionContract(t *testing.T) {
	if os.Getenv("KODEX_PROXY_SESSION_CONTAINER_TEST") != "1" {
		t.Skip("explicit local container profile required")
	}
	logger.SetOutput(io.Discard)
	logger.SetErrOutput(io.Discard)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	root := t.TempDir()
	for _, name := range []string{"auth", "tls", "wrong-tls", "config", "data"} {
		must(t, os.Mkdir(filepath.Join(root, name), 0700))
	}
	var bundle struct {
		Items []struct {
			Kind string
			Data map[string]string
			Spec struct {
				Template struct {
					Spec struct{ Containers []struct{ Image string } }
				}
			}
		}
	}
	raw, err := os.ReadFile("../../../deploy/k8s/base/proxy-session-store/resources.json")
	must(t, err)
	must(t, json.Unmarshal(raw, &bundle))
	image := ""
	for _, item := range bundle.Items {
		if item.Kind == "ConfigMap" {
			for k, v := range item.Data {
				must(t, os.WriteFile(filepath.Join(root, "config", k), []byte(v), 0600))
			}
		}
		if item.Kind == "StatefulSet" {
			image = item.Spec.Template.Spec.Containers[0].Image
		}
	}
	if !strings.Contains(image, "@sha256:") {
		t.Fatal("immutable fixture image required")
	}
	script := `import {sessionStoreSecret} from '../proxy-session-store.mjs'; import{writeFileSync}from'node:fs';const s=sessionStoreSecret();for(const[k,v]of Object.entries(s.stringData))writeFileSync(process.argv[1]+'/'+k,v,{mode:0o600,flag:'wx'});`
	cmd := exec.CommandContext(ctx, "node", "--input-type=module", "-e", script, filepath.Join(root, "auth"))
	if out, e := cmd.CombinedOutput(); e != nil {
		_ = out
		t.Fatal("fixture auth preparation failed")
	}
	makeTLS(t, filepath.Join(root, "tls"))
	makeTLS(t, filepath.Join(root, "wrong-tls"))
	name := "kodex-proxy-contract-" + strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	docker := func(args ...string) []byte {
		t.Helper()
		out, e := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if e != nil {
			t.Fatalf("local container command failed: %s", args[0])
		}
		return out
	}
	args := []string{"run", "-d", "--name", name, "--user", "0:0", "-p", "127.0.0.1::6379", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges"}
	for _, dir := range []string{"auth", "tls", "config", "data"} {
		mount := filepath.Join(root, dir) + ":/" + dir
		if dir != "data" {
			mount += ":ro"
		}
		args = append(args, "-v", mount)
	}
	args = append(args, "--entrypoint", "valkey-server", image, "/config/valkey.conf")
	docker(args...)
	defer func() { _ = exec.Command("docker", "rm", "-f", name).Run() }()
	var inspect []struct {
		NetworkSettings struct {
			Ports map[string][]struct{ HostPort string }
		}
	}
	must(t, json.Unmarshal(docker("inspect", name), &inspect))
	if len(inspect[0].NetworkSettings.Ports["6379/tcp"]) == 0 {
		t.Fatal("fixture container terminated before port allocation")
	}
	port := inspect[0].NetworkSettings.Ports["6379/tcp"][0].HostPort
	password, e := os.ReadFile(filepath.Join(root, "auth", "proxy-password"))
	must(t, e)
	opts := &options.SessionOptions{Redis: options.RedisStoreOptions{ConnectionURL: "rediss://localhost:" + port + "/0", Username: "proxy", Password: string(password), CAPath: filepath.Join(root, "tls", "ca.crt")}}
	cookie := &options.Cookie{Name: "_kodex_control_center_oauth2", Secret: strings.Repeat("x", 32), Expire: 8 * time.Hour, Secure: true, HTTPOnly: true, Path: "/"}
	newStore := func() sessionsapi.SessionStore {
		s, e := redisstore.NewRedisSessionStore(opts, cookie)
		must(t, e)
		return s
	}
	restart := func() {
		docker("restart", name)
		must(t, json.Unmarshal(docker("inspect", name), &inspect))
		if len(inspect[0].NetworkSettings.Ports["6379/tcp"]) == 0 {
			t.Fatal("restart port absent")
		}
		opts.Redis.ConnectionURL = "rediss://localhost:" + inspect[0].NetworkSettings.Ports["6379/tcp"][0].HostPort + "/0"
	}
	first := newStore()
	deadline := time.Now().Add(15 * time.Second)
	for first.VerifyConnection(ctx) != nil {
		if time.Now().After(deadline) {
			logs, _ := exec.Command("docker", "logs", name).CombinedOutput()
			if strings.Contains(string(logs), "Permission denied") {
				t.Fatal("TLS fixture permission denied")
			}
			if strings.Contains(string(logs), "Error loading ACLs") {
				t.Fatal("TLS fixture ACL rejected")
			}
			if strings.Contains(string(logs), "Unrecognized option") {
				t.Fatal("TLS fixture config option rejected")
			}
			t.Fatal("TLS authenticated store not ready")
		}
		time.Sleep(100 * time.Millisecond)
	}
	docker("exec", name, "sh", "/config/ops.sh", "ready")
	t.Run("cookie_store_reproduces_concurrent_rotating_refresh", func(t *testing.T) {
		a, e := cookiestore.NewCookieSessionStore(opts, cookie)
		must(t, e)
		b, e := cookiestore.NewCookieSessionStore(opts, cookie)
		must(t, e)
		runConcurrent(t, a, b, 2)
	})
	t.Run("two_proxy_instances_one_refresh", func(t *testing.T) { runConcurrent(t, newStore(), newStore(), 1) })
	t.Run("fresh_session_does_not_refresh", func(t *testing.T) { runConcurrent(t, newStore(), newStore(), 0) })
	t.Run("temporary_authority_outage_keeps_exact_valid_credentials", func(t *testing.T) {
		store := newStore()
		req, e := save(t, store)
		must(t, e)
		before, e := store.Load(req)
		must(t, e)
		accepted := consume(store, req, func(context.Context, *sessionsapi.SessionState) (bool, error) {
			return false, errors.New("synthetic temporary outage")
		})
		if !accepted {
			t.Fatal("valid last known credentials rejected")
		}
		after, e := store.Load(req)
		must(t, e)
		if before.RefreshToken != after.RefreshToken || !before.CreatedAt.Equal(*after.CreatedAt) || !before.ExpiresOn.Equal(*after.ExpiresOn) {
			t.Fatal("failed refresh changed accepted state")
		}
	})
	t.Run("authoritative_revoke_clears_session", func(t *testing.T) {
		store := newStore()
		req, e := save(t, store)
		must(t, e)
		if consume(store, req, func(context.Context, *sessionsapi.SessionState) (bool, error) { return false, &fixtureRefreshError{} }) {
			t.Fatal("revoked session accepted")
		}
		if _, e = store.Load(req); e == nil {
			t.Fatal("revoked state retained")
		}
	})
	t.Run("TLS_wrong_CA_closed", func(t *testing.T) {
		bad := *opts
		bad.Redis.CAPath = filepath.Join(root, "wrong-tls", "ca.crt")
		s, e := redisstore.NewRedisSessionStore(&bad, cookie)
		must(t, e)
		if s.VerifyConnection(ctx) == nil {
			t.Fatal("untrusted issuer accepted")
		}
	})
	t.Run("wrong_password_closed", func(t *testing.T) {
		bad := *opts
		bad.Redis.Password = "invalid-fixture"
		s, e := redisstore.NewRedisSessionStore(&bad, cookie)
		must(t, e)
		if s.VerifyConnection(ctx) == nil {
			t.Fatal("wrong password accepted")
		}
	})
	t.Run("ACL_other_namespace_closed", func(t *testing.T) {
		other := *cookie
		other.Name = "unrelated"
		s, e := redisstore.NewRedisSessionStore(opts, &other)
		must(t, e)
		_, err := save(t, s)
		if err == nil {
			t.Fatal("foreign key scope accepted")
		}
	})
	t.Run("restart_keeps_exact_session_and_clear_is_durable", func(t *testing.T) {
		s := newStore()
		req, e := save(t, s)
		must(t, e)
		restart()
		for newStore().VerifyConnection(ctx) != nil {
			if time.Now().After(deadline.Add(30 * time.Second)) {
				t.Fatal("restart recovery timeout")
			}
			time.Sleep(100 * time.Millisecond)
		}
		if _, e = newStore().Load(req); e != nil {
			t.Fatal("durable session lost")
		}
		must(t, newStore().Clear(httptest.NewRecorder(), req))
		restart()
		time.Sleep(500 * time.Millisecond)
		if _, e = newStore().Load(req); e == nil {
			t.Fatal("cleared session restored")
		}
	})
	t.Run("KNOWN_LIMITATION_slow_refresh_exceeds_vendor_lease_NOT_CLOSED", func(t *testing.T) {
		runConcurrent(t, newStore(), newStore(), 2, 2300*time.Millisecond)
		t.Log("KNOWN_LIMITATION: vendor lease permits duplicate rotating refresh after 2 seconds; resilience NOT CLOSED")
	})
	t.Run("store_outage_closed", func(t *testing.T) {
		s := newStore()
		req, e := save(t, s)
		must(t, e)
		docker("stop", name)
		if _, e = s.Load(req); e == nil {
			t.Fatal("unavailable store accepted session")
		}
	})
}
func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal("fixture operation failed")
	}
}
func save(t *testing.T, s sessionsapi.SessionStore) (*http.Request, error) {
	t.Helper()
	req := httptest.NewRequest("GET", "https://control.fixture.test/", nil)
	rec := httptest.NewRecorder()
	created := time.Now().Add(-time.Hour - time.Second)
	expiry := time.Now().Add(time.Hour)
	state := &sessionsapi.SessionState{CreatedAt: &created, ExpiresOn: &expiry, AccessToken: "synthetic-access", RefreshToken: "synthetic-initial", User: "synthetic"}
	if e := s.Save(rec, req, state); e != nil {
		return nil, e
	}
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	return req, nil
}
func runConcurrent(t *testing.T, a, b sessionsapi.SessionStore, expected int32, delay ...time.Duration) {
	t.Helper()
	req, e := save(t, a)
	must(t, e)
	if expected == 0 {
		current, e := a.Load(req)
		must(t, e)
		current.CreatedAtNow()
		must(t, a.Save(httptest.NewRecorder(), req, current))
	}
	var calls atomic.Int32
	var successes atomic.Int32
	start := make(chan struct{})
	var wg sync.WaitGroup
	refresh := func(_ context.Context, s *sessionsapi.SessionState) (bool, error) {
		n := calls.Add(1)
		duration := 75 * time.Millisecond
		if len(delay) > 0 {
			duration = delay[0]
		}
		time.Sleep(duration)
		if n > 1 {
			return false, &fixtureRefreshError{}
		}
		s.RefreshToken = "synthetic-next"
		return true, nil
	}
	for _, store := range []sessionsapi.SessionStore{a, b} {
		wg.Add(1)
		go func(s sessionsapi.SessionStore) {
			defer wg.Done()
			<-start
			handler := middleware.NewScope(false, "", nil)(middleware.NewStoredSessionLoader(&middleware.StoredSessionLoaderOptions{SessionStore: s, RefreshPeriod: time.Hour, RefreshSession: refresh, ValidateSession: func(context.Context, *sessionsapi.SessionState) bool { return true }})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if middlewareapi.GetRequestScope(r).Session != nil {
					successes.Add(1)
					w.WriteHeader(202)
				} else {
					w.WriteHeader(401)
				}
			})))
			handler.ServeHTTP(httptest.NewRecorder(), req.Clone(context.Background()))
		}(store)
	}
	started := time.Now()
	close(start)
	wg.Wait()
	if expected == 1 && time.Since(started) > time.Second {
		t.Fatal("session lock was not released promptly")
	}
	if calls.Load() != expected {
		t.Fatalf("refresh cardinality actual=%d expected=%d", calls.Load(), expected)
	}
	want := int32(2)
	if expected == 2 {
		want = 1
	}
	if successes.Load() != want {
		t.Fatalf("accepted request cardinality actual=%d expected=%d", successes.Load(), want)
	}
}

func consume(store sessionsapi.SessionStore, req *http.Request, refresh func(context.Context, *sessionsapi.SessionState) (bool, error)) bool {
	accepted := false
	handler := middleware.NewScope(false, "", nil)(middleware.NewStoredSessionLoader(&middleware.StoredSessionLoaderOptions{SessionStore: store, RefreshPeriod: time.Hour, RefreshSession: refresh, ValidateSession: func(_ context.Context, s *sessionsapi.SessionState) bool {
		return s.ExpiresOn != nil && time.Now().Before(*s.ExpiresOn)
	}})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accepted = middlewareapi.GetRequestScope(r).Session != nil
	})))
	handler.ServeHTTP(httptest.NewRecorder(), req)
	return accepted
}

type fixtureRefreshError struct{}

func (*fixtureRefreshError) Error() string { return "invalid_grant" }
func makeTLS(t *testing.T, dir string) {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	must(t, e)
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "fixture CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, e := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	must(t, e)
	must(t, os.WriteFile(filepath.Join(dir, "ca.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600))
	serverKey, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	must(t, e)
	cert := &x509.Certificate{SerialNumber: big.NewInt(2), NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), DNSNames: []string{"localhost", "proxy-session-store.kodex-system.svc.cluster.local"}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, e = x509.CreateCertificate(rand.Reader, cert, ca, &serverKey.PublicKey, key)
	must(t, e)
	encoded := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	must(t, os.WriteFile(filepath.Join(dir, "tls.crt"), encoded, 0600))
	must(t, os.WriteFile(filepath.Join(dir, "server.crt"), encoded, 0600))
	raw, e := x509.MarshalECPrivateKey(serverKey)
	must(t, e)
	must(t, os.WriteFile(filepath.Join(dir, "tls.key"), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: raw}), 0600))
}
