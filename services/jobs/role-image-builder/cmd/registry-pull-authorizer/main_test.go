package main

import "testing"

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
