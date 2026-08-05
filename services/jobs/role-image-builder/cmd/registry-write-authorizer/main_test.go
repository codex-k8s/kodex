package main

import "testing"

func TestAllowedRegistryWriteIsClosed(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"/v2/staging/role-images/blobs/uploads/", "/v2/staging/role-images/manifests/build-1",
		"/v2/staging/readiness/manifests/rootless-probe",
	} {
		if !allowedRegistryWrite("PUT", path) {
			t.Fatalf("expected staging write rejected: %s", path)
		}
	}
	for _, path := range []string{
		"/v2/other/manifests/latest", "/v2/staging/role-images/tags/list", "/v2/_catalog",
	} {
		if allowedRegistryWrite("PUT", path) {
			t.Fatalf("out-of-scope write accepted: %s", path)
		}
	}
	if allowedRegistryWrite("DELETE", "/v2/staging/role-images/manifests/digest") {
		t.Fatal("delete was accepted")
	}
}
