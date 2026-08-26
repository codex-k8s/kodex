package main

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
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
