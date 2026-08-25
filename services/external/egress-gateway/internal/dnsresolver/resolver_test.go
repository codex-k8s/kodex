package dnsresolver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
	"github.com/miekg/dns"
)

func TestValidateAddressesRejectsSpecialPurposeAndMixedSnapshots(t *testing.T) {
	prohibited := []string{
		"0.0.0.1", "10.0.0.1", "100.64.0.1", "127.0.0.1", "169.254.1.1", "172.16.0.1",
		"192.0.0.9", "192.0.2.1", "192.31.196.1", "192.52.193.1", "192.88.99.1",
		"192.168.1.1", "192.175.48.1", "198.18.0.1", "198.51.100.1", "203.0.113.1",
		"224.0.0.1", "240.0.0.1", "::", "::1", "::ffff:8.8.8.8", "64:ff9b::808:808",
		"64:ff9b:1::1", "100::1", "100:0:0:1::1", "2001::1", "2001:db8::1", "2002::1",
		"2620:4f:8000::1", "3fff::1", "5f00::1", "fc00::1", "fe80::1", "fec0::1", "ff02::1",
		"::192.168.1.1", "1000::1", "4000::1", "8000::1",
	}
	for _, value := range prohibited {
		if err := ValidateAddresses([]netip.Addr{netip.MustParseAddr(value)}); err == nil {
			t.Fatalf("expected %s to be prohibited", value)
		}
	}
	if err := ValidateAddresses([]netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("10.0.0.1")}); err == nil {
		t.Fatal("expected mixed public/private answer to be rejected")
	}
	if err := ValidateAddresses([]netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("4000::1")}); err == nil {
		t.Fatal("expected mixed public/reserved IPv6 answer to be rejected")
	}
	if err := ValidateAddresses([]netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("2606:4700:4700::1111")}); err != nil {
		t.Fatalf("unexpected public address rejection: %v", err)
	}
}

func TestResolverUsesTTLCacheAndRejectsRebinding(t *testing.T) {
	now := time.Unix(1_000, 0)
	exchange := &sequenceExchanger{a: []string{"93.184.216.34", "10.0.0.1"}, ttl: 1}
	resolver := newTestResolver(t, exchange)
	resolver.now = func() time.Time { return now }
	first, err := resolver.Resolve(context.Background(), "api.openai.com")
	if err != nil || len(first.Addresses) != 1 || exchange.aCalls != 1 {
		t.Fatalf("unexpected first resolution: %+v, %v", first, err)
	}
	if _, err := resolver.Resolve(context.Background(), "api.openai.com"); err != nil || exchange.aCalls != 1 {
		t.Fatalf("cache was not used: %v", err)
	}
	now = now.Add(2 * time.Second)
	_, err = resolver.Resolve(context.Background(), "api.openai.com")
	var resolutionErr *Error
	if !errors.As(err, &resolutionErr) || resolutionErr.Reason != ReasonSpecial || exchange.aCalls != 2 || resolver.Healthy() {
		t.Fatalf("rebinding did not fail closed: %v", err)
	}
}

func TestResolverDoesNotCacheZeroTTLAnswer(t *testing.T) {
	exchange := &sequenceExchanger{a: []string{"93.184.216.34"}, ttl: 0}
	resolver := newTestResolver(t, exchange)
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := resolver.Resolve(context.Background(), "api.openai.com"); err == nil {
			t.Fatal("zero TTL answer must fail closed instead of entering cache")
		}
	}
	if exchange.aCalls != 2 {
		t.Fatalf("zero TTL answer was unexpectedly cached: calls=%d", exchange.aCalls)
	}
}

func TestResolverFollowsBoundedCNAMEAndRecoversTruncationOverTCP(t *testing.T) {
	exchange := &cnameExchanger{}
	resolver := newTestResolver(t, exchange)
	snapshot, err := resolver.Resolve(context.Background(), "api.openai.com")
	if err != nil || len(snapshot.Addresses) != 2 {
		t.Fatalf("unexpected CNAME resolution: %+v, %v", snapshot, err)
	}
	if exchange.tcpCalls == 0 {
		t.Fatal("truncated UDP response was not retried over TCP")
	}
}

func TestResolverRejectsCNAMEOverflow(t *testing.T) {
	for _, exchange := range []Exchanger{&loopExchanger{}, &longChainExchanger{}} {
		resolver := newTestResolver(t, exchange)
		if _, err := resolver.Resolve(context.Background(), "api.openai.com"); err == nil {
			t.Fatal("expected CNAME loop/depth rejection")
		}
	}
}

func TestResolverRejectsAnswerRecordOverflow(t *testing.T) {
	resolver := newTestResolver(t, &recordOverflowExchanger{})
	_, err := resolver.Resolve(context.Background(), "api.openai.com")
	var resolutionErr *Error
	if !errors.As(err, &resolutionErr) || resolutionErr.Reason != ReasonBounds {
		t.Fatalf("unexpected record overflow result: %v", err)
	}
}

func newTestResolver(t *testing.T, exchanger Exchanger) *Resolver {
	t.Helper()
	config := policy.DNSConfig{
		MinimumTTLSeconds: 1, MaximumTTLSeconds: 30, MaximumCacheEntries: 8,
		MaximumQueries: 8, MaximumCNAMEDepth: 4, MaximumRecords: 16,
		MaximumMessageBytes: 4096, QueryTimeoutMilliseconds: 500,
	}
	resolver, err := New(config, []netip.AddrPort{netip.MustParseAddrPort("127.0.0.53:53")}, exchanger, nil)
	if err != nil {
		t.Fatal(err)
	}
	return resolver
}

type sequenceExchanger struct {
	a      []string
	ttl    uint32
	aCalls int
}

func (exchange *sequenceExchanger) Exchange(_ context.Context, request *dns.Msg, _ netip.AddrPort, _ string) (*dns.Msg, error) {
	response := new(dns.Msg)
	response.SetReply(request)
	if request.Question[0].Qtype == dns.TypeA {
		index := exchange.aCalls
		if index >= len(exchange.a) {
			index = len(exchange.a) - 1
		}
		exchange.aCalls++
		response.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: request.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: exchange.ttl}, A: net.ParseIP(exchange.a[index]).To4()}}
	}
	return response, nil
}

type cnameExchanger struct{ tcpCalls int }

func (exchange *cnameExchanger) Exchange(_ context.Context, request *dns.Msg, _ netip.AddrPort, network string) (*dns.Msg, error) {
	response := new(dns.Msg)
	response.SetReply(request)
	question := request.Question[0]
	if question.Qtype == dns.TypeA && network == "udp" {
		response.Truncated = true
		return response, nil
	}
	if network == "tcp" {
		exchange.tcpCalls++
	}
	if question.Name == "api.openai.com." {
		response.Answer = []dns.RR{&dns.CNAME{Hdr: dns.RR_Header{Name: question.Name, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 20}, Target: "edge.example.net."}}
		return response, nil
	}
	if question.Qtype == dns.TypeA {
		response.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: question.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 10}, A: net.ParseIP("93.184.216.34").To4()}}
	} else {
		response.Answer = []dns.RR{&dns.AAAA{Hdr: dns.RR_Header{Name: question.Name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 10}, AAAA: net.ParseIP("2606:4700:4700::1111")}}
	}
	return response, nil
}

type loopExchanger struct{}

func (*loopExchanger) Exchange(_ context.Context, request *dns.Msg, _ netip.AddrPort, _ string) (*dns.Msg, error) {
	response := new(dns.Msg)
	response.SetReply(request)
	name := request.Question[0].Name
	target := "api.openai.com."
	if name == target {
		target = "loop.example.net."
	}
	response.Answer = []dns.RR{&dns.CNAME{Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 10}, Target: target}}
	return response, nil
}

type longChainExchanger struct{ count int }

func (exchange *longChainExchanger) Exchange(_ context.Context, request *dns.Msg, _ netip.AddrPort, _ string) (*dns.Msg, error) {
	response := new(dns.Msg)
	response.SetReply(request)
	exchange.count++
	target := fmt.Sprintf("chain-%d.example.net.", exchange.count)
	response.Answer = []dns.RR{&dns.CNAME{Hdr: dns.RR_Header{Name: request.Question[0].Name, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 10}, Target: target}}
	return response, nil
}

type recordOverflowExchanger struct{}

func (*recordOverflowExchanger) Exchange(_ context.Context, request *dns.Msg, _ netip.AddrPort, _ string) (*dns.Msg, error) {
	response := new(dns.Msg)
	response.SetReply(request)
	for index := 0; index < 17; index++ {
		response.Answer = append(response.Answer, &dns.A{Hdr: dns.RR_Header{Name: request.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 10}, A: net.ParseIP("93.184.216.34").To4()})
	}
	return response, nil
}
