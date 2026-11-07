package integrationpolicy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	shared "github.com/codex-k8s/kodex/libs/go/integrationegresspolicy"
)

// TestNativePolicyIsStrictAndIndependentFromPublicDNS binds exact private pins without
// letting unknown, duplicate, stale, oversized or multi-document policy bytes enable a listener.
// TestNativePolicyIsStrictAndIndependentFromPublicDNS закрепляет точные private pins без
// допуска неизвестных, повторных, устаревших, чрезмерных либо множественных документов.
func TestNativePolicyIsStrictAndIndependentFromPublicDNS(t *testing.T) {
	doc := shared.NativeDocument{Schema: shared.NativeSchema, Generation: 1, GatewayPolicyDigest: base(t).Digest(), Destinations: []shared.NativeDestination{{Hostname: "gitlab.example.invalid", Port: 443, AddressCIDRs: []string{"10.77.0.4/32"}}}}
	raw, _ := json.Marshal(doc)
	active, err := LoadNative(raw, doc.Digest(), base(t))
	if err != nil || !active.Allows("gitlab.example.invalid", 443) || active.Allows("other.example.invalid", 443) || active.Allows("gitlab.example.invalid", 80) || active.Limits() != base(t).Limits() {
		t.Fatal("native policy escaped immutable authority", err)
	}
	route := active.NativeIntegrationRoute("gitlab.example.invalid")
	route.AddressCIDRs[0] = "10.77.0.5/32"
	if active.NativeIntegrationRoute("gitlab.example.invalid").AddressCIDRs[0] != "10.77.0.4/32" {
		t.Fatal("caller mutated active native route")
	}
	if ready, _ := active.Ready(); !ready {
		t.Fatal("valid native projection not ready")
	}
	if ready, _ := active.ReadyFor("other.example.invalid"); ready {
		t.Fatal("unknown native target ready")
	}
	for name, value := range map[string][]byte{"trailing": append(append([]byte{}, raw...), raw...), "unknown": append([]byte(`{"extra":true,`), raw[1:]...), "duplicate": []byte(`{"schema":"one","schema":"two"}`), "oversize": []byte(strings.Repeat("x", shared.MaximumFileBytes+1))} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadNative(value, doc.Digest(), base(t)); err == nil {
				t.Fatal("invalid native document accepted")
			}
		})
	}
	if _, err := LoadNative(raw, strings.Repeat("b", 64), base(t)); err == nil {
		t.Fatal("wrong expected digest accepted")
	}
	foreign := doc
	foreign.GatewayPolicyDigest = strings.Repeat("c", 64)
	changed, _ := json.Marshal(foreign)
	if _, err := LoadNative(changed, foreign.Digest(), base(t)); err == nil {
		t.Fatal("foreign core policy accepted")
	}
	path := filepath.Join(t.TempDir(), "native.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadNativeFile(path, doc.Digest(), base(t)); err != nil {
		t.Fatal(err)
	}
	missing, err := LoadNativeFile(path+".missing", doc.Digest(), base(t))
	if err == nil {
		t.Fatal("missing optional file accepted")
	}
	if ready, _ := missing.Ready(); ready {
		t.Fatal("invalid optional projection became ready")
	}
}
