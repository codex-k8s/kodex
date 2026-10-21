// Package dnsresolver resolves bounded A/AAAA and CNAME chains through
// server-owned DNS endpoints, preserving authoritative TTLs and rejecting
// incomplete or unsafe answers instead of serving stale snapshots.
// RU: Пакет dnsresolver разрешает ограниченные цепочки A/AAAA и CNAME через
// серверные DNS-серверы, сохраняет исходные TTL и отклоняет неполные либо
// небезопасные ответы вместо выдачи устаревших снимков.
package dnsresolver

import (
	"context"
	"errors"
	"io"
	"net/netip"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	shared "github.com/codex-k8s/kodex/libs/go/mailpolicy"
	"github.com/miekg/dns"
)

// maximumResolverConfigBytes limits reads of the server-owned resolver file.
// RU: maximumResolverConfigBytes ограничивает чтение серверного файла DNS.
const maximumResolverConfigBytes = 16 << 10

// Reason is a closed set of DNS failure categories exposed to observers.
// It cannot carry a hostname, address or arbitrary provider response.
// RU: Reason задаёт закрытый набор причин ошибок DNS для наблюдателя;
// в нём нет имени узла, адреса или произвольного ответа провайдера.
type Reason string

const (
	// ReasonNone denotes a successfully validated snapshot or cache hit.
	// RU: ReasonNone означает проверенный снимок либо попадание в кэш.
	ReasonNone Reason = "none"
	// ReasonTimeout covers cancelled or expired resolution and exchange failures.
	// RU: ReasonTimeout охватывает отменённое или истёкшее разрешение и сбои обмена.
	ReasonTimeout Reason = "timeout"
	// ReasonNXDOMAIN records an authoritative name-error DNS response.
	// RU: ReasonNXDOMAIN обозначает авторитетный DNS-ответ об отсутствии имени.
	ReasonNXDOMAIN Reason = "nxdomain"
	// ReasonTruncated denotes a failed TCP retry after a truncated UDP answer.
	// RU: ReasonTruncated означает неудачный TCP-повтор усечённого UDP-ответа.
	ReasonTruncated Reason = "truncated"
	// ReasonMalformed rejects inconsistent DNS names, records or response fields.
	// RU: ReasonMalformed отклоняет несогласованные имена, записи или поля DNS-ответа.
	ReasonMalformed Reason = "malformed"
	// ReasonBounds covers query, CNAME, record and message-size limits.
	// RU: ReasonBounds означает превышение лимитов запросов, CNAME, записей или размера.
	ReasonBounds Reason = "bounds"
	// ReasonSpecial rejects a snapshot containing any non-public address.
	// RU: ReasonSpecial отклоняет снимок с любым непубличным адресом.
	ReasonSpecial Reason = "special_address"
	// ReasonEmpty rejects a snapshot without usable A or AAAA addresses.
	// RU: ReasonEmpty отклоняет снимок без пригодных адресов A или AAAA.
	ReasonEmpty Reason = "empty"
)

// Error carries only the closed reason; its string omits DNS names, resolver
// endpoints and answer records so rejection can be reported without leaking them.
// RU: Error содержит только закрытую причину; его строка не раскрывает имена,
// адреса DNS-серверов и записи ответа при сообщении об отказе.
type Error struct{ Reason Reason }

// Error formats the fixed DNS rejection prefix and its closed reason category.
// RU: Error формирует постоянный префикс отказа DNS и закрытую категорию причины.
func (err *Error) Error() string { return "DNS resolution rejected: " + string(err.Reason) }

// Snapshot aliases the shared immutable-by-contract address set and exact
// expiry so egress and the control plane interpret the same DNS provenance.
// RU: Snapshot ссылается на общий набор адресов и точный срок действия,
// чтобы egress и control plane одинаково понимали происхождение DNS-данных.
type Snapshot = shared.Snapshot

// Exchanger executes one DNS exchange against a literal resolver endpoint.
// The caller supplies the bounded context, message and UDP/TCP network.
// RU: Exchanger выполняет один DNS-обмен с буквальным адресом сервера;
// вызывающий код задаёт ограниченный контекст, сообщение и UDP либо TCP.
type Exchanger interface {
	// Exchange returns one response or error without choosing another server.
	// RU: Exchange возвращает один ответ или ошибку, не выбирая другой сервер.
	Exchange(context.Context, *dns.Msg, netip.AddrPort, string) (*dns.Msg, error)
}

// Observer receives only bounded outcome and reason labels, never query data.
// RU: Observer получает только ограниченные метки исхода и причины, без данных запроса.
type Observer func(outcome string, reason Reason)

// Resolver owns A/AAAA lookups, CNAME traversal, a bounded cache and the last
// validation health bit; server addresses are copied at construction.
// RU: Resolver владеет запросами A/AAAA, переходами CNAME, ограниченным кэшем
// и признаком последней проверки; адреса серверов копируются при создании.
type Resolver struct {
	config    Config
	servers   []netip.AddrPort
	exchanger Exchanger
	now       func() time.Time
	observe   Observer
	healthy   atomic.Bool
	cacheMu   sync.Mutex
	cache     map[string]Snapshot
}

// New validates all limits and one to three literal port-53 DNS servers before
// creating a resolver. A nil exchanger uses the network client; a nil observer
// becomes a no-op, so neither changes the validation policy.
// RU: New проверяет все лимиты и от одного до трёх буквальных адресов DNS на
// порту53. При nil обменнике используется сетевой клиент, а nil наблюдатель
// не выполняет действий; это не меняет политику проверки.
func New(config Config, servers []netip.AddrPort, exchanger Exchanger, observe Observer) (*Resolver, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if len(servers) == 0 || len(servers) > 3 {
		return nil, errors.New("DNS server configuration is invalid")
	}
	for _, server := range servers {
		if !server.IsValid() || server.Addr().IsUnspecified() || server.Port() != 53 {
			return nil, errors.New("DNS server configuration is invalid")
		}
	}
	if exchanger == nil {
		exchanger = networkExchanger{timeout: time.Duration(config.QueryTimeoutMilliseconds) * time.Millisecond}
	}
	if observe == nil {
		observe = func(string, Reason) {}
	}
	return &Resolver{
		config: config, servers: append([]netip.AddrPort(nil), servers...), exchanger: exchanger,
		now: time.Now, observe: observe, cache: make(map[string]Snapshot),
	}, nil
}

// LoadSystemServers reads at most 16 KiB from the server-selected resolver
// file, deduplicates up to three literal nameservers and assigns port 53.
// It rejects malformed entries without returning resolver addresses in errors.
// RU: LoadSystemServers читает не более16 КиБ из выбранного сервером файла,
// убирает повторы, принимает до трёх буквальных IP DNS и назначает порт53.
// Некорректные записи отклоняются без раскрытия адресов в ошибках.
func LoadSystemServers(path string) ([]netip.AddrPort, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("open resolver configuration")
	}
	defer file.Close()
	value, err := io.ReadAll(io.LimitReader(file, maximumResolverConfigBytes+1))
	if err != nil || len(value) == 0 || len(value) > maximumResolverConfigBytes {
		return nil, errors.New("resolver configuration size is invalid")
	}
	servers := make([]netip.AddrPort, 0, 3)
	seen := map[netip.Addr]struct{}{}
	for _, line := range strings.Split(string(value), "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == "nameserver" {
			if len(fields) != 2 {
				return nil, errors.New("resolver nameserver entry is invalid")
			}
			address, parseErr := netip.ParseAddr(fields[1])
			if parseErr != nil || address.IsUnspecified() || address.Is4In6() {
				return nil, errors.New("resolver nameserver is not a valid IP literal")
			}
			if _, exists := seen[address]; exists {
				continue
			}
			if len(servers) == 3 {
				return nil, errors.New("resolver nameserver count exceeds bound")
			}
			seen[address] = struct{}{}
			servers = append(servers, netip.AddrPortFrom(address, 53))
		}
	}
	if len(servers) == 0 {
		return nil, errors.New("resolver configuration has no nameserver")
	}
	return servers, nil
}

// Healthy reports whether the most recent resolution completed validation;
// a nil resolver is unhealthy, and every rejection clears the bit.
// RU: Healthy сообщает, прошла ли последняя попытка разрешения проверку;
// nil resolver считается нездоровым, любой отказ сбрасывает признак.
func (resolver *Resolver) Healthy() bool { return resolver != nil && resolver.healthy.Load() }

// Resolve returns a fully validated snapshot, reusing only an unexpired cache
// entry and rechecking its addresses; it has no stale fallback.
// RU: Resolve возвращает полностью проверенный снимок: использует только
// действующий кэш и повторно проверяет его адреса, без устаревшего fallback.
func (resolver *Resolver) Resolve(ctx context.Context, hostname string) (Snapshot, error) {
	return resolver.resolve(ctx, hostname, false)
}

// Refresh bypasses the cache and replaces it only after a complete DNS check;
// on failure it discards the old entry instead of extending its lifetime.
// RU: Refresh обходит кэш и заменяет запись лишь после полной проверки DNS;
// при отказе удаляет прежнюю запись вместо продления её срока.
func (resolver *Resolver) Refresh(ctx context.Context, hostname string) (Snapshot, error) {
	return resolver.resolve(ctx, hostname, true)
}

// resolve normalizes the host, validates both address families and publishes a
// snapshot with the shortest authoritative TTL; only sufficiently long answers
// enter the cache, and a cancelled or expired result is never returned.
// RU: resolve нормализует имя, проверяет оба семейства адресов и выдаёт снимок
// с наименьшим исходным TTL. В кэш попадают только достаточно долгие ответы;
// отменённый либо истёкший результат не выдаётся.
func (resolver *Resolver) resolve(ctx context.Context, hostname string, refresh bool) (Snapshot, error) {
	if ctx.Err() != nil {
		return resolver.reject(ReasonTimeout)
	}
	hostname, err := shared.NormalizeHostname(hostname)
	if err != nil {
		return resolver.reject(ReasonMalformed)
	}
	reject := func(reason Reason) (Snapshot, error) {
		if refresh {
			resolver.discard(hostname)
		}
		return resolver.reject(reason)
	}
	now := resolver.now()
	resolver.cacheMu.Lock()
	cached, exists := resolver.cache[hostname]
	if !refresh && exists && now.Before(cached.ExpiresAt) {
		cached.Addresses = append([]netip.Addr(nil), cached.Addresses...)
		resolver.cacheMu.Unlock()
		if err := ValidateAddresses(cached.Addresses); err != nil {
			resolver.discard(hostname)
			return reject(ReasonSpecial)
		}
		resolver.healthy.Store(true)
		resolver.observe("cache_hit", ReasonNone)
		return cached, nil
	}
	if exists && !now.Before(cached.ExpiresAt) {
		delete(resolver.cache, hostname)
	}
	resolver.cacheMu.Unlock()

	queries := 0
	addressesA, ttlA, err := resolver.resolveType(ctx, hostname, dns.TypeA, &queries)
	if err != nil {
		return reject(errorReason(err))
	}
	addressesAAAA, ttlAAAA, err := resolver.resolveType(ctx, hostname, dns.TypeAAAA, &queries)
	if err != nil {
		return reject(errorReason(err))
	}
	addresses := append(addressesA, addressesAAAA...)
	if len(addresses) == 0 {
		return reject(ReasonEmpty)
	}
	addresses = uniqueSorted(addresses)
	if err := ValidateAddresses(addresses); err != nil {
		return reject(ReasonSpecial)
	}
	ttl := minimumResolvedTTL(ttlA, ttlAAAA)
	if ttl <= 0 {
		return reject(ReasonMalformed)
	}
	minimumTTL := time.Duration(resolver.config.MinimumTTLSeconds) * time.Second
	maximumTTL := time.Duration(resolver.config.MaximumTTLSeconds) * time.Second
	if ttl > maximumTTL {
		ttl = maximumTTL
	}
	snapshot := Snapshot{Addresses: append([]netip.Addr(nil), addresses...), ExpiresAt: now.Add(ttl)}
	if ctx.Err() != nil || !resolver.now().Before(snapshot.ExpiresAt) {
		return reject(ReasonTimeout)
	}
	// Нижний предел относится к кэшу, но не продлевает TTL авторитетного DNS.
	if ttl >= minimumTTL {
		resolver.cacheMu.Lock()
		resolver.store(hostname, snapshot)
		resolver.cacheMu.Unlock()
	} else if refresh {
		resolver.discard(hostname)
	}
	resolver.healthy.Store(true)
	resolver.observe("validated", ReasonNone)
	return snapshot, nil
}

// discard removes one cached host under its mutex after a failed refresh.
// RU: discard удаляет одно имя из кэша под блокировкой после неудачного обновления.
func (resolver *Resolver) discard(hostname string) {
	resolver.cacheMu.Lock()
	delete(resolver.cache, hostname)
	resolver.cacheMu.Unlock()
}

// resolveType follows one address family through bounded CNAME hops, counting
// each exchange across both families and rejecting loops or unrelated owners.
// It returns a negative TTL only when that family has no address records.
// RU: resolveType проходит одно семейство через ограниченную цепочку CNAME,
// суммарно считает запросы обоих семейств и отклоняет циклы и посторонние имена.
// Отрицательный TTL возвращается, лишь когда у семейства нет адресных записей.
func (resolver *Resolver) resolveType(ctx context.Context, hostname string, queryType uint16, queries *int) ([]netip.Addr, time.Duration, error) {
	current := hostname
	seen := map[string]struct{}{current: {}}
	minimumTTL := time.Duration(-1)
	depth := 0
	for {
		if *queries >= resolver.config.MaximumQueries {
			return nil, 0, &Error{Reason: ReasonBounds}
		}
		*queries = *queries + 1
		response, err := resolver.query(ctx, current, queryType)
		if err != nil {
			return nil, 0, err
		}
		cname, addresses, ttlByOwner, err := resolver.parseAnswers(response, queryType)
		if err != nil {
			return nil, 0, err
		}
		used := map[string]struct{}{}
		for {
			used[current] = struct{}{}
			if values := addresses[current]; len(values) > 0 {
				minimumTTL = minTTL(minimumTTL, ttlByOwner[current])
				if !onlyUsedOwners(cname, addresses, used) {
					return nil, 0, &Error{Reason: ReasonMalformed}
				}
				return values, minimumTTL, nil
			}
			target, exists := cname[current]
			if !exists {
				if len(cname) != 0 || len(addresses) != 0 {
					return nil, 0, &Error{Reason: ReasonMalformed}
				}
				return nil, -1, nil
			}
			minimumTTL = minTTL(minimumTTL, ttlByOwner[current])
			depth++
			if depth > resolver.config.MaximumCNAMEDepth {
				return nil, 0, &Error{Reason: ReasonBounds}
			}
			if _, duplicate := seen[target]; duplicate {
				return nil, 0, &Error{Reason: ReasonMalformed}
			}
			seen[target] = struct{}{}
			current = target
			if _, targetCNAME := cname[current]; !targetCNAME && len(addresses[current]) == 0 {
				if !onlyUsedOwners(cname, addresses, used) {
					return nil, 0, &Error{Reason: ReasonMalformed}
				}
				break
			}
		}
	}
}

// query tries configured servers in order, retries truncated UDP answers over
// TCP with a fresh deadline, then validates the matching response envelope.
// Transport failures are classified without leaking server or answer details.
// RU: query перебирает серверы по порядку, повторяет усечённый UDP через TCP
// с новым сроком ожидания и проверяет соответствие DNS-ответа запросу.
// Сбои транспорта классифицируются без раскрытия деталей сервера и ответа.
func (resolver *Resolver) query(ctx context.Context, hostname string, queryType uint16) (*dns.Msg, error) {
	request := new(dns.Msg)
	request.SetQuestion(dns.Fqdn(hostname), queryType)
	request.RecursionDesired = true
	var last error
	for _, server := range resolver.servers {
		queryCtx, cancel := context.WithTimeout(ctx, time.Duration(resolver.config.QueryTimeoutMilliseconds)*time.Millisecond)
		response, err := resolver.exchanger.Exchange(queryCtx, request, server, "udp")
		cancel()
		if err != nil {
			last = &Error{Reason: ReasonTimeout}
			continue
		}
		if response != nil && response.Truncated {
			queryCtx, cancel = context.WithTimeout(ctx, time.Duration(resolver.config.QueryTimeoutMilliseconds)*time.Millisecond)
			response, err = resolver.exchanger.Exchange(queryCtx, request, server, "tcp")
			cancel()
			if err != nil || response == nil || response.Truncated {
				last = &Error{Reason: ReasonTruncated}
				continue
			}
		}
		if err := resolver.validateResponse(request, response); err != nil {
			return nil, err
		}
		return response, nil
	}
	if last != nil {
		return nil, last
	}
	return nil, &Error{Reason: ReasonTimeout}
}

// validateResponse requires the original question and transaction ID, rejects
// DNS errors other than NXDOMAIN separately, and enforces record/byte bounds.
// RU: validateResponse требует исходный вопрос и ID транзакции, отдельно
// обозначает NXDOMAIN и ограничивает число записей и размер DNS-ответа.
func (resolver *Resolver) validateResponse(request, response *dns.Msg) error {
	if response == nil || !response.Response || response.Id != request.Id || response.Opcode != dns.OpcodeQuery ||
		len(response.Question) != 1 || response.Question[0] != request.Question[0] {
		return &Error{Reason: ReasonMalformed}
	}
	if response.Rcode == dns.RcodeNameError {
		return &Error{Reason: ReasonNXDOMAIN}
	}
	if response.Rcode != dns.RcodeSuccess {
		return &Error{Reason: ReasonMalformed}
	}
	if len(response.Answer)+len(response.Ns)+len(response.Extra) > resolver.config.MaximumRecords || response.Len() > resolver.config.MaximumMessageBytes {
		return &Error{Reason: ReasonBounds}
	}
	return nil
}

// parseAnswers accepts only Internet-class CNAME and the requested A or AAAA
// records, retaining the minimum TTL per owner and rejecting mixed record
// kinds, invalid addresses and unexpected answer types.
// RU: parseAnswers принимает только записи Internet-class CNAME и запрошенные
// A либо AAAA, хранит минимальный TTL каждого имени и отклоняет смешанные
// типы записей, некорректные адреса и неожиданные типы ответа.
func (resolver *Resolver) parseAnswers(response *dns.Msg, queryType uint16) (map[string]string, map[string][]netip.Addr, map[string]time.Duration, error) {
	cnames := map[string]string{}
	addresses := map[string][]netip.Addr{}
	ttls := map[string]time.Duration{}
	for _, answer := range response.Answer {
		header := answer.Header()
		if header.Class != dns.ClassINET {
			return nil, nil, nil, &Error{Reason: ReasonMalformed}
		}
		owner, err := normalizeDNSName(header.Name)
		if err != nil {
			return nil, nil, nil, err
		}
		ttl := time.Duration(header.Ttl) * time.Second
		if current, exists := ttls[owner]; !exists || ttl < current {
			ttls[owner] = ttl
		}
		switch value := answer.(type) {
		case *dns.CNAME:
			target, normalizeErr := normalizeDNSName(value.Target)
			if normalizeErr != nil {
				return nil, nil, nil, normalizeErr
			}
			if _, exists := cnames[owner]; exists || len(addresses[owner]) != 0 {
				return nil, nil, nil, &Error{Reason: ReasonMalformed}
			}
			cnames[owner] = target
		case *dns.A:
			if queryType != dns.TypeA || len(cnames[owner]) != 0 {
				return nil, nil, nil, &Error{Reason: ReasonMalformed}
			}
			address, ok := netip.AddrFromSlice(value.A)
			if !ok || !address.Is4() {
				return nil, nil, nil, &Error{Reason: ReasonMalformed}
			}
			addresses[owner] = append(addresses[owner], address)
		case *dns.AAAA:
			if queryType != dns.TypeAAAA || len(cnames[owner]) != 0 {
				return nil, nil, nil, &Error{Reason: ReasonMalformed}
			}
			address, ok := netip.AddrFromSlice(value.AAAA)
			if !ok || !address.Is6() || address.Is4In6() {
				return nil, nil, nil, &Error{Reason: ReasonMalformed}
			}
			addresses[owner] = append(addresses[owner], address)
		default:
			return nil, nil, nil, &Error{Reason: ReasonMalformed}
		}
	}
	return cnames, addresses, ttls, nil
}

// reject clears the last-health flag, emits only a closed reason and returns
// no partial address snapshot to the caller.
// RU: reject сбрасывает признак здоровья, сообщает лишь закрытую причину
// и не возвращает вызывающему коду частичный снимок адресов.
func (resolver *Resolver) reject(reason Reason) (Snapshot, error) {
	resolver.healthy.Store(false)
	resolver.observe("rejected", reason)
	return Snapshot{}, &Error{Reason: reason}
}

// store copies addresses into the cache and evicts the earliest-expiring
// entry at capacity; callers must hold cacheMu throughout this operation.
// RU: store копирует адреса в кэш и при заполнении удаляет запись с ближайшим
// сроком окончания; вызывающий код удерживает cacheMu на всём протяжении.
func (resolver *Resolver) store(hostname string, snapshot Snapshot) {
	snapshot.Addresses = append([]netip.Addr(nil), snapshot.Addresses...)
	if len(resolver.cache) >= resolver.config.MaximumCacheEntries {
		oldestName := ""
		var oldestExpiry time.Time
		for name, entry := range resolver.cache {
			if oldestName == "" || entry.ExpiresAt.Before(oldestExpiry) {
				oldestName, oldestExpiry = name, entry.ExpiresAt
			}
		}
		delete(resolver.cache, oldestName)
	}
	resolver.cache[hostname] = snapshot
}

// networkExchanger performs one context-bound UDP or TCP exchange through
// miekg/dns, with a client timeout and a bounded UDP message size.
// RU: networkExchanger выполняет один UDP- либо TCP-обмен через miekg/dns
// с контекстом, сроком ожидания клиента и ограниченным размером UDP-сообщения.
type networkExchanger struct{ timeout time.Duration }

// Exchange sends the given message to the selected literal server and returns
// its response without selecting another endpoint or validating DNS semantics.
// RU: Exchange отправляет сообщение выбранному буквальному адресу сервера и
// возвращает ответ, не выбирая другой сервер и не проверяя смысл DNS-записей.
func (exchanger networkExchanger) Exchange(ctx context.Context, request *dns.Msg, server netip.AddrPort, network string) (*dns.Msg, error) {
	client := &dns.Client{Net: network, Timeout: exchanger.timeout, UDPSize: 1232}
	response, _, err := client.ExchangeContext(ctx, request, server.String())
	return response, err
}

// normalizeDNSName removes the final DNS dot and applies the shared hostname
// policy; invalid names become a closed malformed-response reason.
// RU: normalizeDNSName убирает конечную точку DNS и применяет общую политику
// имён; неправильное имя превращается в закрытую причину неверного ответа.
func normalizeDNSName(value string) (string, error) {
	value = strings.TrimSuffix(value, ".")
	hostname, err := shared.NormalizeHostname(value)
	if err != nil {
		return "", &Error{Reason: ReasonMalformed}
	}
	return hostname, nil
}

// errorReason preserves typed resolver reasons and maps all unrelated errors
// to malformed rather than exposing arbitrary error strings to observers.
// RU: errorReason сохраняет типизированные причины resolver, а остальные
// ошибки относит к malformed без передачи произвольного текста наблюдателю.
func errorReason(err error) Reason {
	var resolutionErr *Error
	if errors.As(err, &resolutionErr) {
		return resolutionErr.Reason
	}
	return ReasonMalformed
}

// onlyUsedOwners requires every CNAME and address owner in one answer to lie
// on the followed chain, so unrelated records cannot influence validation.
// RU: onlyUsedOwners требует, чтобы каждое имя CNAME и адреса в ответе входило
// в пройденную цепочку, исключая влияние посторонних записей на проверку.
func onlyUsedOwners(cnames map[string]string, addresses map[string][]netip.Addr, used map[string]struct{}) bool {
	for owner := range cnames {
		if _, ok := used[owner]; !ok {
			return false
		}
	}
	for owner := range addresses {
		if _, ok := used[owner]; !ok {
			return false
		}
	}
	return true
}

// minTTL folds owner TTLs across one CNAME chain, treating a negative current
// value as an unset accumulator.
// RU: minTTL сводит TTL имён одной цепочки CNAME; отрицательное текущее
// значение означает ещё не заданный накопитель.
func minTTL(current, candidate time.Duration) time.Duration {
	if current < 0 || candidate < current {
		return candidate
	}
	return current
}

// minimumResolvedTTL combines A and AAAA TTLs while treating a negative
// family result as absent rather than as a short authoritative expiry.
// RU: minimumResolvedTTL объединяет TTL A и AAAA, считая отрицательный
// результат семейства отсутствием, а не коротким сроком действия.
func minimumResolvedTTL(left, right time.Duration) time.Duration {
	if left < 0 {
		return right
	}
	if right < 0 || left < right {
		return left
	}
	return right
}

// uniqueSorted removes repeated addresses and sorts the remaining IPv4/IPv6
// values for a stable snapshot independent of DNS answer order.
// RU: uniqueSorted убирает повторяющиеся адреса и сортирует оставшиеся IPv4/IPv6
// для стабильного снимка независимо от порядка записей в DNS-ответе.
func uniqueSorted(values []netip.Addr) []netip.Addr {
	seen := make(map[netip.Addr]struct{}, len(values))
	result := make([]netip.Addr, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; !exists {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Less(result[right]) })
	return result
}
