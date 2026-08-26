package main

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net/http"
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
