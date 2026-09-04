package runtimecontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func validWorkspacePolicy() RuntimeWorkspacePolicy {
	return RuntimeWorkspacePolicyV1()
}

func TestRuntimeWorkspacePolicyNormalizesAndAppliesLongestPrefix(t *testing.T) {
	policy := validWorkspacePolicy()
	for _, test := range []struct{ candidate, access, denial string }{
		{".kodex/outbox/result.md", RuntimeWorkspaceWritable, ""},
		{"/workspace/input/set/files/a.txt", RuntimeWorkspaceReadOnly, ""},
		{"/workspace/knowledge/memory.md", RuntimeWorkspaceReadOnly, ""},
		{"/workspace/.kodex/state/codex-home/auth.json", RuntimeWorkspaceReadOnly, ""},
		{"../other/session", "", RuntimeWorkspacePathOutsideWorkspace},
		{"/workspace/input/../../other", "", RuntimeWorkspacePathOutsideWorkspace},
		{"/other/project", "", RuntimeWorkspacePathOutsideWorkspace},
	} {
		access, denial := policy.AccessForPath(test.candidate)
		if access != test.access || denial != test.denial {
			t.Errorf("AccessForPath(%q) = (%q, %q), want (%q, %q)", test.candidate, access, denial, test.access, test.denial)
		}
	}
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
