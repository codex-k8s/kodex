package runtimepolicy

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

func TestRuntimePolicyDoesNotTreatGenericBaseDestinationsAsProviderAuthority(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "..", "deploy/k8s/base/egress-gateway/policy.json")
	digest, err := policy.DigestFile(path)
	if err != nil {
		t.Fatal(err)
	}
	base, err := policy.LoadFile(path, "2026-09-05.1", digest)
	if err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("synthetic", 8))
	active, err := New(base, key)
	if err != nil {
		t.Fatal(err)
	}
	for _, web := range []runtimecontract.RuntimeWebAccess{
		{Mode: runtimecontract.RuntimeWebAccessNone, Rules: []runtimecontract.RuntimeWebAccessRule{}},
		{Mode: runtimecontract.RuntimeWebAccessAllowlistReadOnly, Rules: []runtimecontract.RuntimeWebAccessRule{{DomainPattern: "example.org", Protocol: "HTTPS", Port: 443, HTTPMethods: []string{"GET"}}}},
	} {
		grant, err := runtimecontract.SignRuntimeWebAccessGrant(key, "synthetic-execution", strings.Repeat("a", 64), web)
		if err != nil {
			t.Fatal(err)
		}
		for _, host := range []string{"api.openai.com", "chatgpt.com", "auth.openai.com"} {
			access, ok := active.AuthorizeAuthenticated(host, 443, grant)
			if !ok || !access.ProviderAccess || access.WebAccess.Mode != web.Mode {
				t.Fatal("provider route inherited user restrictions")
			}
		}
		for _, host := range []string{"github.com", "api.github.com", "unknown.example", "127.0.0.1"} {
			if _, ok := active.AuthorizeAuthenticated(host, 443, grant); ok {
				t.Fatal("generic destination bypassed user policy")
			}
		}
		_, ok := active.AuthorizeAuthenticated("example.org", 443, grant)
		if ok != (web.Mode != runtimecontract.RuntimeWebAccessNone) {
			t.Fatal("user route eligibility changed")
		}
		for _, port := range []int{80, 444} {
			if _, ok := active.AuthorizeAuthenticated("api.openai.com", port, grant); ok {
				t.Fatal("provider alternate port accepted")
			}
		}
		if _, ok := active.AuthorizeAuthenticated("api.openai.com", 443, grant+"tampered"); ok {
			t.Fatal("provider route skipped grant verification")
		}
	}
}
