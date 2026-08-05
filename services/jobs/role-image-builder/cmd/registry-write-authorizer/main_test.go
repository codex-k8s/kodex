package main

import (
	"net/http"
	"testing"
)

func TestAllowedRegistryWriteIsClosed(t *testing.T) {
	t.Parallel()
	for _, request := range []struct{ method, path string }{
		{"POST", "/v2/staging/role-images/blobs/uploads/"},
		{"PATCH", "/v2/staging/role-images/blobs/uploads/id"},
		{"PUT", "/v2/staging/role-images/blobs/uploads/id"},
		{"PUT", "/v2/staging/role-images/manifests/build-1"},
		{"HEAD", "/v2/staging/role-images/blobs/sha256:abc"},
		{"PUT", "/v2/staging/readiness/manifests/rootless-probe"},
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
		{"mattercodex-image-signer", http.MethodPut, "/v2/evidence/role-image-admission/manifests/latest"},
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
