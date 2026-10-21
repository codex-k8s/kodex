package dnsresolver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// TestValidateAddressesRejectsSpecialPurposeAndMixedSnapshots checks the shared
// public-address boundary for reserved IPv4/IPv6 and mixed DNS answer sets.
// RU: TestValidateAddressesRejectsSpecialPurposeAndMixedSnapshots проверяет
// отказ для зарезервированных IPv4/IPv6 и смешанных DNS-наборов.
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

// TestResolverUsesTTLCacheAndRejectsRebinding advances the clock across a TTL
// to prove a cached public answer cannot mask a later private DNS answer.
// RU: TestResolverUsesTTLCacheAndRejectsRebinding переводит часы через TTL
// и проверяет, что публичный кэш не скрывает последующий частный DNS-адрес.
func TestResolverUsesTTLCacheAndRejectsRebinding(t *testing.T) {
	now := time.Unix(1_000, 0)
	exchange := &sequenceExchanger{a: []string{"93.184.216.34", "10.0.0.1"}, ttl: 5}
	resolver := newTestResolver(t, exchange)
	resolver.now = func() time.Time { return now }
	first, err := resolver.Resolve(context.Background(), "api.openai.com")
	if err != nil || len(first.Addresses) != 1 || exchange.aCalls != 1 {
		t.Fatalf("unexpected first resolution: %+v, %v", first, err)
	}
	if _, err := resolver.Resolve(context.Background(), "api.openai.com"); err != nil || exchange.aCalls != 1 {
		t.Fatalf("cache was not used: %v", err)
	}
	now = now.Add(6 * time.Second)
	_, err = resolver.Resolve(context.Background(), "api.openai.com")
	var resolutionErr *Error
	if !errors.As(err, &resolutionErr) || resolutionErr.Reason != ReasonSpecial || exchange.aCalls != 2 || resolver.Healthy() {
		t.Fatalf("rebinding did not fail closed: %v", err)
	}
}

// TestResolverDoesNotCacheZeroTTLAnswer requires zero-TTL responses to fail
// closed on every lookup rather than being reused from a cache entry.
// RU: TestResolverDoesNotCacheZeroTTLAnswer требует закрытого отказа для
// нулевого TTL при каждом поиске вместо повторной выдачи записи кэша.
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

// TestResolverDoesNotExtendShortTTLOrShareCachedAddresses checks that a short
// authoritative TTL is not stretched by the cache floor and that callers
// cannot mutate the cached address slice through a returned snapshot.
// RU: TestResolverDoesNotExtendShortTTLOrShareCachedAddresses проверяет, что
// нижний предел кэша не продлевает короткий TTL и вызывающий код не меняет
// хранимые адреса через полученный снимок.
func TestResolverDoesNotExtendShortTTLOrShareCachedAddresses(t *testing.T) {
	now := time.Unix(1_000, 0)
	exchange := &sequenceExchanger{a: []string{"93.184.216.34"}, ttl: 1}
	resolver := newTestResolver(t, exchange)
	resolver.now = func() time.Time { return now }
	for attempt := 0; attempt < 2; attempt++ {
		snapshot, err := resolver.Resolve(t.Context(), "api.openai.com")
		if err != nil || !snapshot.ExpiresAt.Equal(now.Add(time.Second)) || len(resolver.cache) != 0 || exchange.aCalls != attempt+1 {
			t.Fatal("short authoritative TTL was extended or cached")
		}
	}
	exchange.ttl = 10
	snapshot, err := resolver.Resolve(t.Context(), "api.openai.com")
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Addresses[0] = netip.MustParseAddr("8.8.8.8")
	readback, err := resolver.Resolve(t.Context(), "api.openai.com")
	if err != nil || readback.Addresses[0].String() != "93.184.216.34" || exchange.aCalls != 3 {
		t.Fatal("returned snapshot modified the authoritative cache")
	}
}

// TestRefreshBypassesAndAtomicallyReplacesValidCache checks both successful
// replacement and eviction of the previous entry after an unsafe refresh.
// RU: TestRefreshBypassesAndAtomicallyReplacesValidCache проверяет замену
// кэша после успеха и удаление прежней записи после небезопасного обновления.
func TestRefreshBypassesAndAtomicallyReplacesValidCache(t *testing.T) {
	now := time.Unix(1_000, 0)
	exchange := &sequenceExchanger{a: []string{"93.184.216.34", "8.8.8.8", "10.0.0.1"}, ttl: 10}
	resolver := newTestResolver(t, exchange)
	resolver.now = func() time.Time { return now }
	first, err := resolver.Resolve(t.Context(), "api.openai.com")
	if err != nil || first.Addresses[0].String() != "93.184.216.34" || exchange.aCalls != 1 {
		t.Fatal("initial cached snapshot unavailable")
	}
	refreshed, err := resolver.Refresh(t.Context(), "api.openai.com")
	if err != nil || refreshed.Addresses[0].String() != "8.8.8.8" || exchange.aCalls != 2 {
		t.Fatal("authoritative refresh reused cached snapshot")
	}
	readback, err := resolver.Resolve(t.Context(), "api.openai.com")
	if err != nil || readback.Addresses[0].String() != "8.8.8.8" || exchange.aCalls != 2 {
		t.Fatal("refreshed snapshot did not replace cache")
	}
	if _, err := resolver.Refresh(t.Context(), "api.openai.com"); err == nil || resolver.Healthy() || len(resolver.cache) != 0 {
		t.Fatal("failed refresh retained a stale cache fallback")
	}
}

// TestResolverRejectsCancelledCacheAndExpiredResolution proves cancellation
// blocks cache reads and an answer that expires in flight never becomes ready.
// RU: TestResolverRejectsCancelledCacheAndExpiredResolution проверяет, что
// отмена блокирует чтение кэша, а ответ, истёкший в пути, не становится готовым.
func TestResolverRejectsCancelledCacheAndExpiredResolution(t *testing.T) {
	now := time.Unix(1_000, 0)
	exchange := &sequenceExchanger{a: []string{"93.184.216.34"}, ttl: 5}
	resolver := newTestResolver(t, exchange)
	resolver.now = func() time.Time { return now }
	if _, err := resolver.Resolve(t.Context(), "api.openai.com"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := resolver.Resolve(ctx, "api.openai.com"); err == nil || resolver.Healthy() || exchange.aCalls != 1 {
		t.Fatal("cancelled context received a cached snapshot")
	}
	resolver = newTestResolver(t, exchange)
	clockCalls := 0
	resolver.now = func() time.Time {
		clockCalls++
		return now.Add(time.Duration(clockCalls-1) * 6 * time.Second)
	}
	if _, err := resolver.Resolve(t.Context(), "api.openai.com"); err == nil || resolver.Healthy() || len(resolver.cache) != 0 {
		t.Fatal("expired resolution entered cache or readiness")
	}
}

// TestResolverConstructorRequiresCompleteBoundedConfiguration varies each
// configured bound and requires the same sentinel before network access.
// RU: TestResolverConstructorRequiresCompleteBoundedConfiguration изменяет
// каждый лимит и ожидает один sentinel до любого обращения к сети.
func TestResolverConstructorRequiresCompleteBoundedConfiguration(t *testing.T) {
	valid := Config{MinimumTTLSeconds: 5, MaximumTTLSeconds: 30, MaximumCacheEntries: 8, MaximumQueries: 8, MaximumCNAMEDepth: 4, MaximumRecords: 16, MaximumMessageBytes: 4096, QueryTimeoutMilliseconds: 500}
	for _, change := range []func(*Config){
		func(c *Config) { *c = Config{} },
		func(c *Config) { c.QueryTimeoutMilliseconds = 100_000 },
		func(c *Config) { c.MaximumQueries = 1_000 },
		func(c *Config) { c.MaximumRecords = 1_000_000 },
		func(c *Config) { c.MaximumMessageBytes = 1 << 30 },
		func(c *Config) { c.MaximumCacheEntries = 1_000_000 },
		func(c *Config) { c.MaximumTTLSeconds = 86_400 },
		func(c *Config) { c.MaximumCNAMEDepth = 1_000 },
	} {
		config := valid
		change(&config)
		if _, err := New(config, []netip.AddrPort{netip.MustParseAddrPort("127.0.0.53:53")}, nil, nil); !errors.Is(err, ErrInvalidConfig) {
			t.Fatal("unbounded resolver configuration accepted")
		}
	}
}

// TestResolverFollowsBoundedCNAMEAndRecoversTruncationOverTCP checks a valid
// CNAME chain, a TCP retry after truncated UDP for A, and both address families.
// RU: TestResolverFollowsBoundedCNAMEAndRecoversTruncationOverTCP проверяет
// допустимую цепочку, TCP-повтор для усечённого A-ответа и оба семейства адресов.
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

// TestResolverRejectsCNAMEOverflow exercises both a repeated target and a
// chain longer than the configured maximum depth.
// RU: TestResolverRejectsCNAMEOverflow проверяет повтор цели CNAME и цепочку,
// длина которой превышает настроенный предел.
func TestResolverRejectsCNAMEOverflow(t *testing.T) {
	for _, exchange := range []Exchanger{&loopExchanger{}, &longChainExchanger{}} {
		resolver := newTestResolver(t, exchange)
		if _, err := resolver.Resolve(context.Background(), "api.openai.com"); err == nil {
			t.Fatal("expected CNAME loop/depth rejection")
		}
	}
}

// TestResolverRejectsAnswerRecordOverflow requires the DNS record cap to
// reject a syntactically valid but oversized answer before it is returned.
// RU: TestResolverRejectsAnswerRecordOverflow требует отказа для допустимого
// по форме ответа с избытком записей до его выдачи вызывающему коду.
func TestResolverRejectsAnswerRecordOverflow(t *testing.T) {
	resolver := newTestResolver(t, &recordOverflowExchanger{})
	_, err := resolver.Resolve(context.Background(), "api.openai.com")
	var resolutionErr *Error
	if !errors.As(err, &resolutionErr) || resolutionErr.Reason != ReasonBounds {
		t.Fatalf("unexpected record overflow result: %v", err)
	}
}

// newTestResolver builds a bounded resolver with an injected exchanger and a
// loopback literal server; fixtures never need public DNS or live providers.
// RU: newTestResolver создаёт ограниченный resolver с подставным обменником
// и буквальным loopback-адресом, без обращения к публичному DNS или провайдеру.
func newTestResolver(t *testing.T, exchanger Exchanger) *Resolver {
	t.Helper()
	config := Config{
		MinimumTTLSeconds: 5, MaximumTTLSeconds: 30, MaximumCacheEntries: 8,
		MaximumQueries: 8, MaximumCNAMEDepth: 4, MaximumRecords: 16,
		MaximumMessageBytes: 4096, QueryTimeoutMilliseconds: 500,
	}
	resolver, err := New(config, []netip.AddrPort{netip.MustParseAddrPort("127.0.0.53:53")}, exchanger, nil)
	if err != nil {
		t.Fatal(err)
	}
	return resolver
}

// sequenceExchanger returns successive A answers to exercise TTL and DNS
// rebinding while recording how often the network path was used.
// RU: sequenceExchanger последовательно выдаёт A-ответы для проверки TTL и
// смены адреса DNS, учитывая число сетевых запросов.
type sequenceExchanger struct {
	a      []string
	ttl    uint32
	aCalls int
}

// Exchange returns the next configured IPv4 address for A and an empty valid
// response for AAAA, with the same transaction and question as the request.
// RU: Exchange возвращает следующий IPv4 для A и пустой допустимый ответ для
// AAAA, сохраняя ID транзакции и вопрос исходного запроса.
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

// cnameExchanger supplies a valid CNAME chain and forces an initial UDP
// truncation so tests can observe the TCP recovery path.
// RU: cnameExchanger выдаёт допустимую цепочку CNAME и принудительно усекает
// первый UDP-ответ для проверки восстановления через TCP.
type cnameExchanger struct{ tcpCalls int }

// Exchange returns a truncated initial UDP answer, then a CNAME or final
// address answer and counts TCP retries on the same test fixture.
// RU: Exchange сначала выдаёт усечённый UDP-ответ, затем CNAME или конечный
// адрес и считает TCP-повторы внутри того же тестового сценария.
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

// loopExchanger alternates two CNAME owners to model a resolvable-looking
// answer that must fail when the target repeats.
// RU: loopExchanger чередует два имени CNAME, имитируя внешне корректный
// ответ, который должен быть отклонён после повторения цели.
type loopExchanger struct{}

// Exchange returns the opposite CNAME owner while preserving the request
// envelope, exposing only the resolver's loop detection to the test.
// RU: Exchange возвращает противоположное имя CNAME с сохранением оболочки
// запроса, чтобы тест проверял именно обнаружение цикла resolver.
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

// longChainExchanger generates fresh CNAME targets until the configured depth
// rejects the chain, keeping a counter per fixture instance.
// RU: longChainExchanger создаёт новые цели CNAME до превышения предела
// глубины, сохраняя счётчик внутри одного тестового экземпляра.
type longChainExchanger struct{ count int }

// Exchange synthesizes one new CNAME target per query with a matching DNS
// reply envelope; it does not use external resolution.
// RU: Exchange создаёт одну новую цель CNAME на запрос с совпадающей
// оболочкой DNS-ответа и не обращается к внешнему resolver.
func (exchange *longChainExchanger) Exchange(_ context.Context, request *dns.Msg, _ netip.AddrPort, _ string) (*dns.Msg, error) {
	response := new(dns.Msg)
	response.SetReply(request)
	exchange.count++
	target := fmt.Sprintf("chain-%d.example.net.", exchange.count)
	response.Answer = []dns.RR{&dns.CNAME{Hdr: dns.RR_Header{Name: request.Question[0].Name, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 10}, Target: target}}
	return response, nil
}

// recordOverflowExchanger returns more A records than the fixture's bound to
// verify rejection before the response is admitted as a snapshot.
// RU: recordOverflowExchanger возвращает больше A-записей, чем разрешено
// тестовым лимитом, для проверки отказа до допуска снимка.
type recordOverflowExchanger struct{}

// Exchange fills an otherwise matching DNS response with seventeen A records,
// one above the fixture limit, without contacting a network endpoint.
// RU: Exchange заполняет совпадающий с запросом DNS-ответ семнадцатью
// A-записями, на одну выше тестового лимита, без обращения к сети.
func (*recordOverflowExchanger) Exchange(_ context.Context, request *dns.Msg, _ netip.AddrPort, _ string) (*dns.Msg, error) {
	response := new(dns.Msg)
	response.SetReply(request)
	for index := 0; index < 17; index++ {
		response.Answer = append(response.Answer, &dns.A{Hdr: dns.RR_Header{Name: request.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 10}, A: net.ParseIP("93.184.216.34").To4()})
	}
	return response, nil
}
