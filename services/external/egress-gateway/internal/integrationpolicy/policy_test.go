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

type resolverFixture struct {
	snapshot  dnsresolver.Snapshot
	snapshots map[string]dnsresolver.Snapshot
}

func (r *resolverFixture) Resolve(_ context.Context, host string) (dnsresolver.Snapshot, error) {
	if r.snapshots != nil {
		return r.snapshots[host], nil
	}
	return r.snapshot, nil
}
func (r *resolverFixture) Refresh(ctx context.Context, host string) (dnsresolver.Snapshot, error) {
	return r.Resolve(ctx, host)
}

func TestReadinessIsIndependentPerIntegrationDestination(t *testing.T) {
	doc := document(t)
	doc.Destinations = append(doc.Destinations, shared.Destination{Hostname: "other.example.test", Port: 443, Addresses: []string{"9.9.9.9"}})
	doc.SourceDigest = shared.SourceDigest([]string{"api.example.test", "other.example.test"})
	raw, _ := json.Marshal(doc)
	active, err := Load(raw, doc.Digest(), base(t))
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(time.Minute)
	resolver := &resolverFixture{snapshots: map[string]dnsresolver.Snapshot{
		"api.example.test":   {Addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8")}, ExpiresAt: expires},
		"other.example.test": {Addresses: []netip.Addr{netip.MustParseAddr("1.1.1.1")}, ExpiresAt: expires},
	}}
	readiness := NewReadiness(active, resolver)
	readiness.Check(t.Context())
	if ready, _ := readiness.Ready(); !ready {
		t.Fatal("healthy destination was blocked by another origin")
	}
	if ready, _ := readiness.ReadyFor("api.example.test"); !ready {
		t.Fatal("healthy destination was rejected")
	}
	for _, host := range []string{"other.example.test", "unknown.example.test"} {
		if ready, _ := readiness.ReadyFor(host); ready {
			t.Fatal("unready or unknown destination was accepted")
		}
	}
	resolver.snapshots["api.example.test"] = dnsresolver.Snapshot{Addresses: []netip.Addr{netip.MustParseAddr("1.1.1.1")}, ExpiresAt: expires}
	readiness.Check(t.Context())
	if ready, _ := readiness.Ready(); ready {
		t.Fatal("all destinations became unavailable without closing listener readiness")
	}
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

func TestReadinessAcceptsOnlyFreshPublicDNSWithPinnedOverlap(t *testing.T) {
	doc := document(t)
	raw, _ := json.Marshal(doc)
	active, err := Load(raw, doc.Digest(), base(t))
	if err != nil {
		t.Fatal(err)
	}
	resolver := &resolverFixture{snapshot: dnsresolver.Snapshot{
		Addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("9.9.9.9")},
		ExpiresAt: time.Now().Add(time.Minute),
	}}
	readiness := NewReadiness(active, resolver)
	readiness.Check(t.Context())
	if ready, _ := readiness.ReadyFor("api.example.test"); !ready {
		t.Fatal("rotating public DNS lost its pinned overlap")
	}
	resolver.snapshot.Addresses = []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("10.0.0.1")}
	readiness.Check(t.Context())
	if ready, _ := readiness.ReadyFor("api.example.test"); ready {
		t.Fatal("mixed private DNS answer passed readiness")
	}
}
