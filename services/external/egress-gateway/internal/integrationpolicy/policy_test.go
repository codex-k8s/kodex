package integrationpolicy

import (
	"context"
	"encoding/json"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/dnsresolver"
	shared "github.com/codex-k8s/kodex/libs/go/integrationegresspolicy"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

func base(t *testing.T) *policy.Active {
	t.Helper()
	path := "../../../../../deploy/k8s/base/egress-gateway/policy.json"
	digest, err := policy.DigestFile(path)
	if err != nil {
		t.Fatal(err)
	}
	active, err := policy.LoadFile(path, "2026-09-05.1", digest)
	if err != nil {
		t.Fatal(err)
	}
	return active
}

func document(t *testing.T) shared.Document {
	t.Helper()
	host := "api.example.test"
	return shared.Document{Schema: shared.Schema, Generation: 1,
		SourceDigest: shared.SourceDigest([]string{host}), GatewayPolicyDigest: base(t).Digest(),
		Destinations: []shared.Destination{{Hostname: host, Port: 443, Addresses: []string{"8.8.8.8"}}}}
}

func TestStrictIntegrationPolicyAndExactLiteral(t *testing.T) {
	doc := document(t)
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	active, err := Load(raw, doc.Digest(), base(t))
	if err != nil || !active.Allows("api.example.test", 443) || active.Allows("api.example.test", 80) ||
		active.Allows("other.example.test", 443) || active.TLSMode("api.example.test", 443) != "implicit" ||
		!active.AllowsLiteral("api.example.test", 443, netip.MustParseAddr("8.8.8.8")) ||
		active.AllowsLiteral("api.example.test", 443, netip.MustParseAddr("1.1.1.1")) {
		t.Fatal("integration listener escaped exact origin or DNS pin", err)
	}
	for name, value := range map[string][]byte{
		"trailing":  append(append([]byte{}, raw...), raw...),
		"duplicate": []byte(`{"schema":"egress-integration/v1","schema":"egress-integration/v1"}`),
		"unknown":   append([]byte(`{"extra":true,`), raw[1:]...),
		"oversize":  []byte(strings.Repeat("x", shared.MaximumFileBytes+1)),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(value, doc.Digest(), base(t)); err == nil {
				t.Fatal("invalid projection accepted")
			}
		})
	}
	doc.GatewayPolicyDigest = strings.Repeat("a", 64)
	foreign, _ := json.Marshal(doc)
	if _, err := Load(foreign, doc.Digest(), base(t)); err == nil {
		t.Fatal("foreign base policy accepted")
	}
}

type resolverFixture struct{ snapshot dnsresolver.Snapshot }

func (r *resolverFixture) Resolve(context.Context, string) (dnsresolver.Snapshot, error) {
	return r.snapshot, nil
}
func (r *resolverFixture) Refresh(context.Context, string) (dnsresolver.Snapshot, error) {
	return r.snapshot, nil
}

func TestReadinessClosesOnDNSRebindingAndEmptyFirstRun(t *testing.T) {
	doc := document(t)
	raw, _ := json.Marshal(doc)
	active, err := Load(raw, doc.Digest(), base(t))
	if err != nil {
		t.Fatal(err)
	}
	resolver := &resolverFixture{snapshot: dnsresolver.Snapshot{Addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8")}, ExpiresAt: time.Now().Add(time.Minute)}}
	readiness := NewReadiness(active, resolver)
	readiness.Check(t.Context())
	if ready, _ := readiness.Ready(); !ready {
		t.Fatal("matching DNS not ready")
	}
	resolver.snapshot.Addresses = []netip.Addr{netip.MustParseAddr("1.1.1.1")}
	readiness.Check(t.Context())
	if ready, _ := readiness.Ready(); ready {
		t.Fatal("rebound DNS remained ready")
	}
	doc.Destinations = []shared.Destination{}
	doc.SourceDigest = shared.SourceDigest(nil)
	raw, _ = json.Marshal(doc)
	active, err = Load(raw, doc.Digest(), base(t))
	if err != nil {
		t.Fatal(err)
	}
	readiness = NewReadiness(active, resolver)
	readiness.Check(t.Context())
	if ready, _ := readiness.Ready(); ready {
		t.Fatal("empty source became ready")
	}
}
