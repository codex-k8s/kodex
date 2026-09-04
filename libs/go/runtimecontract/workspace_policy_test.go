package runtimecontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func validWorkspacePolicy() RuntimeWorkspacePolicy {
	p := RuntimeWorkspacePolicy{Revision: 1, Root: "/workspace", Rules: []RuntimeWorkspacePathRule{{Path: "/workspace/input", Access: "READ_ONLY"}, {Path: "/workspace", Access: "WRITABLE"}}, MaximumWritableBytes: 1024, MaximumFileCount: 10, DenialReasons: []string{"READ_ONLY", "QUOTA_EXCEEDED", "PATH_OUTSIDE_WORKSPACE", "RUNTIME_IO_ERROR"}}
	raw, _ := json.Marshal(p)
	sum := sha256.Sum256(raw)
	p.Digest = hex.EncodeToString(sum[:])
	return p
}

func TestRuntimeWorkspacePolicyRejectsDigestAndUnsafePath(t *testing.T) {
	p := validWorkspacePolicy()
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	p.Digest = "bad"
	if err := p.Validate(); err == nil {
		t.Fatal("digest mismatch accepted")
	}
	p = validWorkspacePolicy()
	p.Rules[0].Path = "/workspace/../etc"
	raw, _ := json.Marshal(p)
	sum := sha256.Sum256(raw)
	p.Digest = hex.EncodeToString(sum[:])
	if err := p.Validate(); err == nil {
		t.Fatal("unsafe path accepted")
	}
}
