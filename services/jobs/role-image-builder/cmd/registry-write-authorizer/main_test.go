package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRegistryProxyTimeoutsBoundLargeBlobStreams(t *testing.T) {
	t.Parallel()
	client := newRegistryProxyClient()
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
	server := newRegistryProxyServer(":0", http.NotFoundHandler(), pool)
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
		t.Fatal("mTLS boundary changed while configuring stream timeouts")
	}
}

func TestAllowedRegistryWriteIsClosed(t *testing.T) {
	t.Parallel()
	for _, request := range []struct{ method, path string }{
		{"POST", "/v2/staging/role-images/blobs/uploads/"},
		{"PATCH", "/v2/staging/role-images/blobs/uploads/id"},
		{"PUT", "/v2/staging/role-images/blobs/uploads/id"},
		{"PUT", "/v2/staging/role-images/manifests/build-1"},
		{"HEAD", "/v2/staging/role-images/blobs/sha256:abc"},
		{"PUT", "/v2/staging/readiness/manifests/userns-probe"},
	} {
		if !allowedRegistryWrite(request.method, request.path) {
			t.Fatalf("expected OCI staging operation rejected: %s %s", request.method, request.path)
		}
	}
	for _, path := range []string{
		"/v2/other/manifests/latest", "/v2/staging/role-images/tags/list", "/v2/_catalog",
		"/v2/staging/role-images/foreign/manifests/latest",
	} {
		if allowedRegistryWrite("PUT", path) {
			t.Fatalf("out-of-scope write accepted: %s", path)
		}
	}
	if allowedRegistryWrite("DELETE", "/v2/staging/role-images/manifests/digest") {
		t.Fatal("delete was accepted")
	}
}

func TestEvidenceAuthorizationIsClosedByIdentityMethodAndRepository(t *testing.T) {
	t.Parallel()
	sequence := []struct{ method, path string }{
		{http.MethodPost, "/v2/evidence/role-image-admission/blobs/uploads/"},
		{http.MethodPatch, "/v2/evidence/role-image-admission/blobs/uploads/id"},
		{http.MethodPut, "/v2/evidence/role-image-admission/blobs/uploads/id?digest=sha256:abc"},
		{http.MethodPut, "/v2/evidence/role-image-admission/manifests/artifact-id"},
		{http.MethodGet, "/v2/evidence/role-image-admission/manifests/sha256:abc"},
	}
	for _, request := range sequence {
		if !allowedEvidenceRequest("image-admission", request.method, request.path) {
			t.Fatalf("OCI evidence operation was rejected: %s %s", request.method, request.path)
		}
	}
	for _, request := range []struct{ commonName, method, path string }{
		{"kodex-image-signer", http.MethodPut, "/v2/evidence/role-image-admission/manifests/latest"},
		{"image-promotion", http.MethodPut, "/v2/evidence/role-image-admission/manifests/latest"},
		{"image-admission", http.MethodDelete, "/v2/evidence/role-image-admission/manifests/sha256:abc"},
		{"image-admission", http.MethodPut, "/v2/staging/role-images/manifests/latest"},
		{"image-admission", http.MethodPut, "/v2/evidence/role-image-admission/foreign/manifests/latest"},
	} {
		if allowedEvidenceRequest(request.commonName, request.method, request.path) {
			t.Fatalf("out-of-scope evidence authority accepted: %+v", request)
		}
	}
	if !allowedEvidenceRequest("image-promotion", http.MethodGet, "/v2/evidence/role-image-admission/manifests/sha256:abc") {
		t.Fatal("promotion exact evidence read was rejected")
	}
}

func TestEvidenceApplicationCredentialIsExact(t *testing.T) {
	t.Parallel()
	reader := func(path string) ([]byte, error) {
		switch {
		case strings.HasSuffix(path, ".username"):
			return []byte("probe-user\n"), nil
		case strings.HasSuffix(path, ".password"):
			return []byte("probe-password\n"), nil
		default:
			return nil, errors.New("unexpected path")
		}
	}
	authorization := "Basic " + base64.StdEncoding.EncodeToString(
		[]byte("probe-user:probe-password"),
	)
	if !authorizedEvidenceCredentialWithReader(
		"kodex-image-registry-evidence-probe",
		authorization,
		reader,
	) {
		t.Fatal("exact evidence probe credential was rejected")
	}
	for _, invalid := range []string{
		"", "basic " + strings.TrimPrefix(authorization, "Basic "), authorization + "x",
	} {
		if authorizedEvidenceCredentialWithReader(
			"kodex-image-registry-evidence-probe",
			invalid,
			reader,
		) {
			t.Fatal("invalid evidence probe credential was accepted")
		}
	}
}

func TestNormalizeMethodHasBoundedCardinality(t *testing.T) {
	t.Parallel()
	if normalizeMethod("UNBOUNDED\nINPUT") != "OTHER" {
		t.Fatal("unbounded method was preserved in diagnostics")
	}
	if normalizeMethod(http.MethodGet) != http.MethodGet {
		t.Fatal("known method was not preserved")
	}
}
