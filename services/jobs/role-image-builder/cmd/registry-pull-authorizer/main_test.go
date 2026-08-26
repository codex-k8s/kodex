package main

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

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
