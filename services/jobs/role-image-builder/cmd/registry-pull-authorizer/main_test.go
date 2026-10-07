package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/jobs/role-image-builder/internal/nodepullidentity"
)

func TestPullRegistryProxyTimeoutsBoundLargeBlobStreams(t *testing.T) {
	t.Parallel()
	client := newPullRegistryProxyClient()
	if client.Timeout != 15*time.Minute {
		t.Fatalf("client timeout = %s, want 15m", client.Timeout)
	}
	if client.CheckRedirect == nil {
		t.Fatal("redirect rejection is not configured")
	}
	if err := client.CheckRedirect(&http.Request{}, nil); err == nil {
		t.Fatal("redirect was accepted")
	}

	pool := x509.NewCertPool()
	server := newPullRegistryProxyServer(":0", http.NotFoundHandler(), pool)
	if server.ReadHeaderTimeout != 5*time.Second ||
		server.ReadTimeout != 15*time.Minute ||
		server.WriteTimeout != 15*time.Minute ||
		server.IdleTimeout != 30*time.Second {
		t.Fatalf(
			"server timeouts = header:%s read:%s write:%s idle:%s",
			server.ReadHeaderTimeout,
			server.ReadTimeout,
			server.WriteTimeout,
			server.IdleTimeout,
		)
	}
	if server.TLSConfig == nil || server.TLSConfig.MinVersion != tls.VersionTLS13 ||
		server.TLSConfig.ClientAuth != tls.RequireAndVerifyClientCert ||
		server.TLSConfig.ClientCAs != pool {
		t.Fatal("mTLS boundary changed while configuring pull stream timeouts")
	}
}

func TestPullClientCertificateAcceptsVerifiedChain(t *testing.T) {
	t.Parallel()
	leaf := &x509.Certificate{Subject: pkix.Name{CommonName: "role-image-builder-input-read"}}
	issuer := &x509.Certificate{Subject: pkix.Name{CommonName: "Kodex installation CA"}}
	request := &http.Request{TLS: &tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf, issuer}}}

	got, failure := pullClientCertificate(request)
	if failure != "" || got != leaf {
		t.Fatalf("verified client chain was rejected: failure=%q certificate=%p", failure, got)
	}
}

func TestPullClientCertificateRejectsMissingTLSIdentity(t *testing.T) {
	t.Parallel()
	if _, failure := pullClientCertificate(&http.Request{}); failure != "tls_state" {
		t.Fatalf("missing TLS state failure = %q", failure)
	}
	if _, failure := pullClientCertificate(&http.Request{TLS: &tls.ConnectionState{}}); failure != "client_certificate" {
		t.Fatalf("missing client certificate failure = %q", failure)
	}
}

func TestNodePullRepositoriesAreClosedToBootstrapAndRuntime(t *testing.T) {
	t.Parallel()
	repositories := []string{"kodex/agent-runner", "kodex/roles"}
	for _, path := range []string{
		"/v2/kodex/agent-runner/manifests/sha256:abc",
		"/v2/kodex/roles/blobs/sha256:abc",
	} {
		if !pathInRepositories(path, repositories) {
			t.Fatalf("required node pull path was rejected: %s", path)
		}
	}
	for _, path := range []string{
		"/v2/kodex/control-plane/manifests/sha256:abc",
		"/v2/evidence/role-image-admission/manifests/sha256:abc",
		"/v2/kodex/roles/tags/list",
	} {
		if pathInRepositories(path, repositories) {
			t.Fatalf("out-of-scope node pull path was accepted: %s", path)
		}
	}
}

func TestInstallationNodePullProfileIsBounded(t *testing.T) {
	t.Parallel()
	profile, ok := pullProfiles()["kodex-node-pull-installer"]
	if !ok {
		t.Fatal("installation node pull profile is absent")
	}
	if profile.configFile != "/identity/pull-dockerconfigjson" {
		t.Fatalf("installation credential path = %q", profile.configFile)
	}
	if len(profile.repositories) != 4 ||
		profile.repositories[0] != "kodex/agent-runner" ||
		profile.repositories[1] != "kodex/roles" ||
		profile.repositories[2] != "kodex/session-archive" ||
		profile.repositories[3] != "kodex/role-image-builder" {
		t.Fatalf("installation repositories = %v", profile.repositories)
	}
}

func TestPlatformArchivePullIsLimitedToNodeIdentities(t *testing.T) {
	t.Parallel()
	for name, profile := range pullProfiles() {
		if allowed := pathInRepositories("/v2/kodex/role-image-builder/manifests/sha256:abc", profile.repositories); allowed != (name == "kodex-node-pull-installer") {
			t.Fatalf("platform builder pull profile %s allowed=%v", name, allowed)
		}
		allowed := pathInRepositories("/v2/kodex/session-archive/manifests/sha256:abc", profile.repositories)
		if allowed != (name == "kodex-node-pull-installer") {
			t.Fatalf("platform archive pull profile %s allowed=%v", name, allowed)
		}
		for _, path := range []string{
			"/v2/kodex/session-archive-other/manifests/sha256:abc",
			"/v2/kodex/session-archive/tags/list",
			"/v2/kodex/role-image-builder-other/manifests/sha256:abc",
			"/v2/kodex/role-image-builder/tags/list",
		} {
			if pathInRepositories(path, profile.repositories) {
				t.Fatalf("unrelated archive path allowed for %s", name)
			}
		}
	}
}

func TestPlatformArchiveActualNodeAuthorization(t *testing.T) {
	t.Parallel()
	const host = "pull.fixture.invalid"
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	name := nodepullidentity.CommonName("fixture-node", 1)
	digest := sha256.Sum256([]byte(name + "\n1\n" + host))
	signature, err := rsa.SignPSS(rand.Reader, key, crypto.SHA256, digest[:], nil)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{Subject: pkix.Name{CommonName: name},
		IPAddresses: []net.IP{net.ParseIP("192.0.2.1")}, PublicKey: &key.PublicKey}
	for _, fixture := range []struct {
		method, path, failure string
	}{
		{http.MethodGet, "/v2/kodex/session-archive/manifests/sha256:abc", ""},
		{http.MethodGet, "/v2/kodex/role-image-builder/manifests/sha256:abc", ""},
		{http.MethodHead, "/v2/kodex/role-image-builder/blobs/sha256:abc", ""},
		{http.MethodPost, "/v2/kodex/role-image-builder/blobs/uploads/", "request_shape"},
		{http.MethodDelete, "/v2/kodex/role-image-builder/manifests/sha256:abc", "request_shape"},
		{http.MethodGet, "/v2/kodex/role-image-builder-other/manifests/sha256:abc", "node_repository"},
		{http.MethodGet, "/v2/kodex/role-image-builder/tags/list", "node_repository"},
		{http.MethodHead, "/v2/kodex/session-archive/blobs/sha256:abc", ""},
		{http.MethodPost, "/v2/kodex/session-archive/blobs/uploads/", "request_shape"},
		{http.MethodDelete, "/v2/kodex/session-archive/manifests/sha256:abc", "request_shape"},
		{http.MethodGet, "/v2/kodex/session-archive-other/manifests/sha256:abc", "node_repository"},
		{http.MethodGet, "/v2/kodex/session-archive/tags/list", "node_repository"},
		{http.MethodGet, "/v2/kodex/control-plane/manifests/sha256:abc", "node_repository"},
	} {
		request := httptest.NewRequest(fixture.method, "https://"+host+fixture.path, nil)
		request.RemoteAddr = "192.0.2.1:1234"
		request.SetBasicAuth(name, "v1.1."+base64.RawURLEncoding.EncodeToString(signature))
		if got := decidePullAuthorization(request, certificate, host); got.failure != fixture.failure {
			t.Fatalf("node path/method decision=%s want=%s", got.failure, fixture.failure)
		}
		request.Header.Del("Authorization")
		if got := decidePullAuthorization(request, certificate, host); got.failure == "" {
			t.Fatal("missing node application credential allowed")
		}
	}
}

func TestPullProfileBasicAuthenticationBoundary(t *testing.T) {
	t.Parallel()
	const registryHost = "images.kodex.works"
	profile := pullProfile{
		configFile: writeDockerConfig(t, map[string]string{
			registryHost: base64.StdEncoding.EncodeToString([]byte("profile-user:profile-password")),
		}),
		repositories: []string{"kodex/dockerfile"},
	}

	tests := []struct {
		name              string
		authorization     string
		wantAllowed       bool
		wantStatus        int
		wantAuthenticate  string
		wantFailureReason string
	}{
		{
			name:              "missing credential receives challenge",
			wantStatus:        http.StatusUnauthorized,
			wantAuthenticate:  pullBasicAuthenticationChallenge,
			wantFailureReason: "profile_credential_missing",
		},
		{
			name:              "wrong credential remains forbidden",
			authorization:     "Basic " + base64.StdEncoding.EncodeToString([]byte("profile-user:wrong-password")),
			wantStatus:        http.StatusForbidden,
			wantFailureReason: "profile_credential",
		},
		{
			name:          "valid credential is allowed",
			authorization: "Basic " + base64.StdEncoding.EncodeToString([]byte("profile-user:profile-password")),
			wantAllowed:   true,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(http.MethodGet, "https://"+registryHost+"/v2/kodex/dockerfile/manifests/sha256:abc", nil)
			if test.authorization != "" {
				request.Header.Set("Authorization", test.authorization)
			}

			decision := decidePullProfileAuthorization(request, profile, registryHost)
			if test.wantAllowed {
				if decision.failure != "" || decision.statusCode != 0 || decision.authChallenge != "" {
					t.Fatalf("valid profile credential was rejected: %+v", decision)
				}
				return
			}
			if decision.failure != test.wantFailureReason {
				t.Fatalf("failure reason = %q, want %q", decision.failure, test.wantFailureReason)
			}
			recorder := httptest.NewRecorder()
			writePullAuthorizationDenial(recorder, decision)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
			if got := recorder.Header().Get("WWW-Authenticate"); got != test.wantAuthenticate {
				t.Fatalf("WWW-Authenticate = %q, want %q", got, test.wantAuthenticate)
			}
			if body := recorder.Body.String(); body != "request denied\n" {
				t.Fatalf("response body exposes unexpected detail: %q", body)
			}
		})
	}
}

func TestPullProfileValidCredentialDoesNotBypassRepositoryBoundary(t *testing.T) {
	t.Parallel()
	const registryHost = "images.kodex.works"
	profile := pullProfile{
		configFile: writeDockerConfig(t, map[string]string{
			registryHost: base64.StdEncoding.EncodeToString([]byte("profile-user:profile-password")),
		}),
		repositories: []string{"kodex/dockerfile"},
	}
	request := httptest.NewRequest(http.MethodGet, "https://"+registryHost+"/v2/kodex/roles/manifests/sha256:abc", nil)
	request.SetBasicAuth("profile-user", "profile-password")

	decision := decidePullProfileAuthorization(request, profile, registryHost)
	if decision.failure != "profile_repository" || decision.statusCode != http.StatusForbidden || decision.authChallenge != "" {
		t.Fatalf("out-of-scope repository decision = %+v", decision)
	}
}

func TestDockerCredentialMatchesInternalAndPromotedHosts(t *testing.T) {
	t.Parallel()
	const promotedHost = "images.kodex.works"
	path := writeDockerConfig(t, map[string]string{
		internalPullRegistryHost: base64.StdEncoding.EncodeToString([]byte("pull-user:pull-password")),
		promotedHost:             base64.StdEncoding.EncodeToString([]byte("pull-user:pull-password")),
	})

	if !dockerCredentialMatches(path, "pull-user", "pull-password", promotedHost) {
		t.Fatal("exact promoted registry credential was rejected")
	}
	if dockerCredentialMatches(path, "pull-user", "wrong-password", promotedHost) {
		t.Fatal("wrong promoted registry credential was accepted")
	}
}

func TestDockerCredentialMatchesInternalProfile(t *testing.T) {
	t.Parallel()
	const promotedHost = "images.kodex.works"
	path := writeDockerConfig(t, map[string]string{
		internalPullRegistryHost: base64.StdEncoding.EncodeToString([]byte("buildkit-user:buildkit-password")),
	})

	if !dockerCredentialMatches(path, "buildkit-user", "buildkit-password", promotedHost) {
		t.Fatal("internal-only profile credential was rejected")
	}
}

func TestDockerCredentialMatchesRejectsUnexpectedRegistryEntry(t *testing.T) {
	t.Parallel()
	const promotedHost = "images.kodex.works"
	auth := base64.StdEncoding.EncodeToString([]byte("pull-user:pull-password"))
	path := writeDockerConfig(t, map[string]string{
		internalPullRegistryHost: auth,
		promotedHost:             auth,
		"unexpected.example":     auth,
	})

	if dockerCredentialMatches(path, "pull-user", "pull-password", promotedHost) {
		t.Fatal("Docker config with an unexpected registry entry was accepted")
	}
}

func writeDockerConfig(t *testing.T, auths map[string]string) string {
	t.Helper()
	document := struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
	}{Auths: make(map[string]struct {
		Auth string `json:"auth"`
	}, len(auths))}
	for host, auth := range auths {
		document.Auths[host] = struct {
			Auth string `json:"auth"`
		}{Auth: auth}
	}
	value, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshal Docker config: %v", err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, value, 0o600); err != nil {
		t.Fatalf("write Docker config: %v", err)
	}
	return path
}
