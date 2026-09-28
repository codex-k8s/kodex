package app

import (
	"context"
	"errors"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/dnsresolver"
)

type rotatingIntegrationDNS struct {
	snapshots []dnsresolver.Snapshot
	index     int
	err       error
}

func (source *rotatingIntegrationDNS) Refresh(_ context.Context, _ string) (dnsresolver.Snapshot, error) {
	if source.err != nil {
		return dnsresolver.Snapshot{}, source.err
	}
	if source.index >= len(source.snapshots) {
		return dnsresolver.Snapshot{}, errors.New("DNS fixture exhausted")
	}
	snapshot := source.snapshots[source.index]
	source.index++
	return snapshot, nil
}

func TestIntegrationDNSProjectionUnionsBoundedRotatingAnswers(t *testing.T) {
	first := netip.MustParseAddr("93.184.215.14")
	second := netip.MustParseAddr("93.184.215.15")
	third := netip.MustParseAddr("2001:4860:4860::8888")
	expiry := time.Now().Add(time.Minute)
	source := &rotatingIntegrationDNS{snapshots: []dnsresolver.Snapshot{
		{Addresses: []netip.Addr{first, third}, ExpiresAt: expiry},
		{Addresses: []netip.Addr{second, third}, ExpiresAt: expiry.Add(time.Second)},
		{Addresses: []netip.Addr{first, second}, ExpiresAt: expiry.Add(2 * time.Second)},
		{Addresses: []netip.Addr{second, third}, ExpiresAt: expiry.Add(3 * time.Second)},
	}}
	actual, err := (&integrationDNSResolver{source: source}).Resolve(t.Context(), "api.example.test")
	if err != nil || source.index != integrationEgressDNSSamples || !actual.ExpiresAt.Equal(expiry) ||
		!reflect.DeepEqual(actual.Addresses, []netip.Addr{first, second, third}) {
		t.Fatalf("bounded DNS union lost a current address: %v", err)
	}
}

func TestIntegrationDNSProjectionRejectsIncompleteSample(t *testing.T) {
	source := &rotatingIntegrationDNS{snapshots: []dnsresolver.Snapshot{{Addresses: []netip.Addr{netip.MustParseAddr("93.184.215.14")}, ExpiresAt: time.Now().Add(time.Minute)}}}
	if _, err := (&integrationDNSResolver{source: source}).Resolve(t.Context(), "api.example.test"); err == nil {
		t.Fatal("incomplete DNS sample was published")
	}
}

func TestIntegrationDNSProjectionRetainsOnlyRecentVerifiedPublicPins(t *testing.T) {
	first := netip.MustParseAddr("8.8.8.8")
	second := netip.MustParseAddr("9.9.9.9")
	start := time.Now()
	source := &rotatingIntegrationDNS{}
	for _, address := range []netip.Addr{first, second, second} {
		for range integrationEgressDNSSamples {
			source.snapshots = append(source.snapshots, dnsresolver.Snapshot{
				Addresses: []netip.Addr{address}, ExpiresAt: start.Add(10 * time.Minute),
			})
		}
	}
	now := start
	resolver := &integrationDNSResolver{source: source, now: func() time.Time { return now }}
	if snapshot, err := resolver.Resolve(t.Context(), "api.example.test"); err != nil ||
		!reflect.DeepEqual(snapshot.Addresses, []netip.Addr{first}) {
		t.Fatal("first verified DNS snapshot was lost", err)
	}
	now = start.Add(time.Minute)
	if snapshot, err := resolver.Resolve(t.Context(), "api.example.test"); err != nil ||
		!reflect.DeepEqual(snapshot.Addresses, []netip.Addr{first, second}) {
		t.Fatal("recent verified DNS pin was not retained", err)
	}
	now = start.Add(integrationEgressDNSOverlap + time.Second)
	if snapshot, err := resolver.Resolve(t.Context(), "api.example.test"); err != nil ||
		!reflect.DeepEqual(snapshot.Addresses, []netip.Addr{second}) {
		t.Fatal("expired DNS pin was retained", err)
	}
}

func TestIntegrationDNSProjectionBoundsRetainedPinsWithoutPublishingPartialChange(t *testing.T) {
	start := time.Now()
	source := &rotatingIntegrationDNS{}
	first := make([]netip.Addr, 0, 32)
	for value := 1; value <= 32; value++ {
		first = append(first, netip.AddrFrom4([4]byte{8, 8, 8, byte(value)}))
	}
	newAddress := netip.MustParseAddr("9.9.9.9")
	for _, addresses := range [][]netip.Addr{first, {newAddress}, {newAddress}} {
		for range integrationEgressDNSSamples {
			source.snapshots = append(source.snapshots, dnsresolver.Snapshot{
				Addresses: addresses, ExpiresAt: start.Add(10 * time.Minute),
			})
		}
	}
	now := start
	resolver := &integrationDNSResolver{source: source, now: func() time.Time { return now }}
	if snapshot, err := resolver.Resolve(t.Context(), "api.example.test"); err != nil || len(snapshot.Addresses) != 32 {
		t.Fatal("initial bounded DNS set was rejected", err)
	}
	now = start.Add(time.Minute)
	if _, err := resolver.Resolve(t.Context(), "api.example.test"); err == nil {
		t.Fatal("overlap published more than 32 addresses")
	}
	if len(resolver.observed["api.example.test"]) != 32 {
		t.Fatal("rejected overlap changed the published DNS set")
	}

	now = start.Add(integrationEgressDNSOverlap + time.Second)
	if snapshot, err := resolver.Resolve(t.Context(), "api.example.test"); err != nil ||
		!reflect.DeepEqual(snapshot.Addresses, []netip.Addr{newAddress}) {
		t.Fatal("expired pins prevented the next bounded DNS publication", err)
	}
}

func TestIntegrationDNSProjectionRejectsMixedPublicPrivateSnapshot(t *testing.T) {
	source := &rotatingIntegrationDNS{snapshots: []dnsresolver.Snapshot{{
		Addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("10.0.0.1")},
		ExpiresAt: time.Now().Add(time.Minute),
	}}}
	if _, err := (&integrationDNSResolver{source: source}).Resolve(t.Context(), "api.example.test"); err == nil {
		t.Fatal("mixed private DNS answer entered overlap state")
	}
}
