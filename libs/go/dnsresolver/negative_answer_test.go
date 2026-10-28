package dnsresolver

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// negativeAnswerExchanger models a recursive cross-zone CNAME response and
// fails redundant target queries, independently of the resolver implementation.
// RU: negativeAnswerExchanger моделирует рекурсивный ответ CNAME в другую зону
// и отклоняет лишние запросы цели независимо от реализации resolver.
type negativeAnswerExchanger struct {
	calls            int
	mutate           func(*dns.Msg)
	transportFailure bool
	allNegative      bool
}

// Exchange returns a public A and a CNAME with terminal SOA for AAAA; a direct
// target lookup fails as in the observed cluster route. No live DNS is used.
// RU: Exchange возвращает публичный A и CNAME с конечной SOA для AAAA; прямой
// запрос цели завершается ошибкой, как в наблюдавшемся маршруте. Живого DNS нет.
func (e *negativeAnswerExchanger) Exchange(_ context.Context, request *dns.Msg, _ netip.AddrPort, _ string) (*dns.Msg, error) {
	e.calls++
	response := new(dns.Msg)
	response.SetReply(request)
	response.RecursionAvailable = true
	question := request.Question[0]
	if question.Name != "service.example.com." {
		response.Rcode = dns.RcodeServerFailure
		return response, nil
	}
	response.Answer = []dns.RR{&dns.CNAME{Hdr: dns.RR_Header{Name: question.Name, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 20}, Target: "edge.example.net."}}
	if question.Qtype == dns.TypeA && !e.allNegative {
		response.Answer = append(response.Answer, &dns.A{Hdr: dns.RR_Header{Name: "edge.example.net.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 30}, A: net.ParseIP("93.184.216.34").To4()})
	} else {
		if e.transportFailure {
			return nil, context.DeadlineExceeded
		}
		response.Ns = []dns.RR{&dns.SOA{Hdr: dns.RR_Header{Name: "example.net.", Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: 10}, Ns: "ns.example.net.", Mbox: "hostmaster.example.net.", Minttl: 7}}
		if e.mutate != nil {
			e.mutate(response)
		}
	}
	return response, nil
}

// TestResolverRequiresAddressesAndFreshNegativeProof rejects all-NODATA and
// in-flight expiry while accepting and validating actual terminal AAAA data.
// RU: TestResolverRequiresAddressesAndFreshNegativeProof отклоняет отсутствие
// обоих семейств и истечение срока в пути, но проверяет реальный конечный AAAA.
func TestResolverRequiresAddressesAndFreshNegativeProof(t *testing.T) {
	t.Run("both_families_absent", func(t *testing.T) {
		exchange := &negativeAnswerExchanger{allNegative: true}
		_, err := newTestResolver(t, exchange).Resolve(t.Context(), "service.example.com")
		var typed *Error
		if !errors.As(err, &typed) || typed.Reason != ReasonEmpty || exchange.calls != 2 {
			t.Fatalf("all-NODATA result was accepted or queried again: %v", err)
		}
	})
	t.Run("negative_proof_expires_in_flight", func(t *testing.T) {
		now := time.Unix(1000, 0)
		exchange := &negativeAnswerExchanger{mutate: func(*dns.Msg) { now = now.Add(8 * time.Second) }}
		resolver := newTestResolver(t, exchange)
		resolver.now = func() time.Time { return now }
		if _, err := resolver.Refresh(t.Context(), "service.example.com"); err == nil || resolver.Healthy() || len(resolver.cache) != 0 {
			t.Fatal("expired negative proof entered cache or readiness")
		}
	})
	t.Run("terminal_ipv6_is_not_nodata", func(t *testing.T) {
		exchange := &negativeAnswerExchanger{mutate: func(r *dns.Msg) {
			r.Answer = append(r.Answer, &dns.AAAA{Hdr: dns.RR_Header{Name: "edge.example.net.", Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 10}, AAAA: net.ParseIP("2606:4700:4700::1111")})
		}}
		result, err := newTestResolver(t, exchange).Resolve(t.Context(), "service.example.com")
		if err != nil || len(result.Addresses) != 2 || exchange.calls != 2 {
			t.Fatalf("terminal IPv6 answer was ignored: %+v %v", result, err)
		}
	})
}

// TestResolverCompletesNegativeCNAMEWithoutRedundantQuery requires the terminal
// SOA to end AAAA resolution and constrain the combined positive snapshot TTL.
// RU: TestResolverCompletesNegativeCNAMEWithoutRedundantQuery требует завершить
// AAAA по конечной SOA и ограничить ею TTL общего положительного снимка.
func TestResolverCompletesNegativeCNAMEWithoutRedundantQuery(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*dns.Msg)
		ttl    time.Duration
	}{
		{name: "recursive_cross_zone", ttl: 7 * time.Second},
		{name: "short_soa_header", ttl: 2 * time.Second, mutate: func(r *dns.Msg) { r.Ns[0].Header().Ttl = 2 }},
		{name: "short_cname", ttl: time.Second, mutate: func(r *dns.Msg) { r.Answer[0].Header().Ttl = 1 }},
		{name: "same_terminal_zone", ttl: 7 * time.Second, mutate: func(r *dns.Msg) { r.Ns[0].Header().Name = "edge.example.net." }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exchange := &negativeAnswerExchanger{mutate: tc.mutate}
			resolver := newTestResolver(t, exchange)
			now := time.Unix(1000, 0)
			resolver.now = func() time.Time { return now }
			result, err := resolver.Resolve(t.Context(), "service.example.com")
			if err != nil || exchange.calls != 2 || len(result.Addresses) != 1 || result.Addresses[0].String() != "93.184.216.34" || !result.ExpiresAt.Equal(now.Add(tc.ttl)) {
				t.Fatalf("terminal negative answer was not respected: calls=%d result=%+v error=%v", exchange.calls, result, err)
			}
			if tc.ttl < 5*time.Second && len(resolver.cache) != 0 {
				t.Fatal("short negative proof entered the cache")
			}
		})
	}
}

// TestResolverRejectsInvalidNegativeProof keeps malformed authority, original
// AAAA failures, unsafe addresses and incomplete chains closed despite valid A.
// RU: TestResolverRejectsInvalidNegativeProof сохраняет закрытый отказ при
// неверной authority, исходной ошибке AAAA, опасном адресе или неполной цепочке.
func TestResolverRejectsInvalidNegativeProof(t *testing.T) {
	for _, tc := range []struct {
		name             string
		mutate           func(*dns.Msg)
		transportFailure bool
	}{
		{name: "foreign_zone", mutate: func(r *dns.Msg) { r.Ns[0].Header().Name = "example.com." }},
		{name: "false_suffix", mutate: func(r *dns.Msg) { r.Ns[0].Header().Name = "ample.net." }},
		{name: "wrong_class", mutate: func(r *dns.Msg) { r.Ns[0].Header().Class = dns.ClassCHAOS }},
		{name: "duplicate_soa", mutate: func(r *dns.Msg) { r.Ns = append(r.Ns, dns.Copy(r.Ns[0])) }},
		{name: "invalid_zone", mutate: func(r *dns.Msg) { r.Ns[0].Header().Name = "bad name." }},
		{name: "invalid_mname", mutate: func(r *dns.Msg) { r.Ns[0].(*dns.SOA).Ns = "bad name." }},
		{name: "invalid_rname", mutate: func(r *dns.Msg) { r.Ns[0].(*dns.SOA).Mbox = "bad name." }},
		{name: "zero_ttl", mutate: func(r *dns.Msg) { r.Ns[0].Header().Ttl = 0 }},
		{name: "zero_minimum", mutate: func(r *dns.Msg) { r.Ns[0].(*dns.SOA).Minttl = 0 }},
		{name: "original_servfail", mutate: func(r *dns.Msg) { r.Rcode = dns.RcodeServerFailure }},
		{name: "original_refused", mutate: func(r *dns.Msg) { r.Rcode = dns.RcodeRefused }},
		{name: "original_nxdomain", mutate: func(r *dns.Msg) { r.Rcode = dns.RcodeNameError }},
		{name: "original_timeout", transportFailure: true},
		{name: "unrelated_answer", mutate: func(r *dns.Msg) {
			r.Answer = append(r.Answer, &dns.CNAME{Hdr: dns.RR_Header{Name: "unrelated.example.net.", Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 10}, Target: "other.example.net."})
		}},
		{name: "cname_loop", mutate: func(r *dns.Msg) { r.Answer[0].(*dns.CNAME).Target = "service.example.com." }},
		{name: "private_ipv6", mutate: func(r *dns.Msg) {
			r.Answer = append(r.Answer, &dns.AAAA{Hdr: dns.RR_Header{Name: "edge.example.net.", Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 10}, AAAA: net.ParseIP("fd00::1")})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exchange := &negativeAnswerExchanger{mutate: tc.mutate, transportFailure: tc.transportFailure}
			resolver := newTestResolver(t, exchange)
			result, err := resolver.Refresh(t.Context(), "service.example.com")
			var typed *Error
			if !errors.As(err, &typed) || len(result.Addresses) != 0 || resolver.Healthy() || len(resolver.cache) != 0 {
				t.Fatalf("invalid negative proof returned partial state: %+v %v", result, err)
			}
		})
	}
}

// TestResolverDoesNotUseNonAuthoritySOA requires a bounded follow-up when no
// terminal authority SOA exists; unrelated sections cannot skip that lookup.
// RU: TestResolverDoesNotUseNonAuthoritySOA требует ограниченный следующий
// запрос без конечной SOA authority; другая секция не отменяет этот запрос.
func TestResolverDoesNotUseNonAuthoritySOA(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*dns.Msg)
	}{
		{name: "no_soa", mutate: func(r *dns.Msg) { r.Ns = nil }},
		{name: "additional_only", mutate: func(r *dns.Msg) { r.Extra = r.Ns; r.Ns = nil }},
		{name: "referral", mutate: func(r *dns.Msg) {
			r.Ns = []dns.RR{&dns.NS{Hdr: dns.RR_Header{Name: "example.net.", Rrtype: dns.TypeNS, Class: dns.ClassINET, Ttl: 10}, Ns: "ns.example.net."}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exchange := &negativeAnswerExchanger{mutate: tc.mutate}
			_, err := newTestResolver(t, exchange).Refresh(t.Context(), "service.example.com")
			if err == nil || exchange.calls != 3 {
				t.Fatalf("missing authority skipped target lookup: calls=%d error=%v", exchange.calls, err)
			}
		})
	}
}
