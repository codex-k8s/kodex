package main

import (
	"strings"
	"testing"
)

func TestNodeReadbackImageUsesTrustedBaseRepositoryAndExactDigest(t *testing.T) {
	t.Parallel()
	valid := "registry.nodes.example.test/kodex/agent-runner@sha256:" + strings.Repeat("a", 64)
	if !validNodeReadbackImage("registry.nodes.example.test", valid) {
		t.Fatal("trusted base node readback image was rejected")
	}
	for _, image := range []string{
		"registry.nodes.example.test/kodex/roles@sha256:" + strings.Repeat("a", 64),
		"registry.nodes.example.test/kodex/control-plane@sha256:" + strings.Repeat("a", 64),
		"registry.nodes.example.test/kodex/agent-runner@sha256:" + strings.Repeat("0", 64),
		valid + "extra", "registry.nodes.example.test/kodex/agent-runner:latest",
	} {
		if validNodeReadbackImage("registry.nodes.example.test", image) {
			t.Fatalf("unsafe node readback image was accepted: %s", image)
		}
	}
}
