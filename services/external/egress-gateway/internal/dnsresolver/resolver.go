// Package dnsresolver реализует server-owned DNS с TTL provenance и fail-closed validation.
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

	"github.com/codex-k8s/matter-codex/services/external/egress-gateway/internal/policy"
	"github.com/miekg/dns"
)

const maximumResolverConfigBytes = 16 << 10

// Reason — закрытый набор исходов DNS validation.
type Reason string

const (
	ReasonNone      Reason = "none"
	ReasonTimeout   Reason = "timeout"
	ReasonNXDOMAIN  Reason = "nxdomain"
	ReasonTruncated Reason = "truncated"
	ReasonMalformed Reason = "malformed"
	ReasonBounds    Reason = "bounds"
	ReasonSpecial   Reason = "special_address"
	ReasonEmpty     Reason = "empty"
)

// Error не раскрывает hostname, resolver address или DNS answer.
type Error struct{ Reason Reason }

func (err *Error) Error() string { return "DNS resolution rejected: " + string(err.Reason) }

// Snapshot — полный проверенный набор literal addresses с bounded expiry.
type Snapshot struct {
	Addresses []netip.Addr
	ExpiresAt time.Time
}

// Exchanger выполняет ровно один DNS exchange к literal resolver address.
type Exchanger interface {
	Exchange(context.Context, *dns.Msg, netip.AddrPort, string) (*dns.Msg, error)
}

// Observer принимает только закрытые outcome/reason.
type Observer func(outcome string, reason Reason)

// Resolver владеет A/AAAA resolution и bounded cache.
type Resolver struct {
	config    policy.DNSConfig
	servers   []netip.AddrPort
	exchanger Exchanger
	now       func() time.Time
	observe   Observer
	healthy   atomic.Bool
	cacheMu   sync.Mutex
	cache     map[string]Snapshot
}

// New создаёт resolver только из literal DNS server addresses.
func New(config policy.DNSConfig, servers []netip.AddrPort, exchanger Exchanger, observe Observer) (*Resolver, error) {
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

// LoadSystemServers bounded-разбирает /etc/resolv.conf и принимает только IP literals.
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

// Healthy сообщает результат последней полной DNS validation.
func (resolver *Resolver) Healthy() bool { return resolver != nil && resolver.healthy.Load() }

// Resolve возвращает только полный validated snapshot; stale fallback отсутствует.
func (resolver *Resolver) Resolve(ctx context.Context, hostname string) (Snapshot, error) {
	hostname, err := policy.NormalizeHostname(hostname)
	if err != nil {
		return resolver.reject(ReasonMalformed)
	}
	now := resolver.now()
	resolver.cacheMu.Lock()
	cached, exists := resolver.cache[hostname]
	if exists && now.Before(cached.ExpiresAt) {
		cached.Addresses = append([]netip.Addr(nil), cached.Addresses...)
		resolver.cacheMu.Unlock()
		if err := ValidateAddresses(cached.Addresses); err != nil {
			resolver.cacheMu.Lock()
			delete(resolver.cache, hostname)
			resolver.cacheMu.Unlock()
			return resolver.reject(ReasonSpecial)
		}
		resolver.healthy.Store(true)
		resolver.observe("cache_hit", ReasonNone)
		return cached, nil
	}
	if exists {
		delete(resolver.cache, hostname)
	}
	resolver.cacheMu.Unlock()

	queries := 0
	addressesA, ttlA, err := resolver.resolveType(ctx, hostname, dns.TypeA, &queries)
	if err != nil {
		return resolver.reject(errorReason(err))
	}
	addressesAAAA, ttlAAAA, err := resolver.resolveType(ctx, hostname, dns.TypeAAAA, &queries)
	if err != nil {
		return resolver.reject(errorReason(err))
	}
	addresses := append(addressesA, addressesAAAA...)
	if len(addresses) == 0 {
		return resolver.reject(ReasonEmpty)
	}
	addresses = uniqueSorted(addresses)
	if err := ValidateAddresses(addresses); err != nil {
		return resolver.reject(ReasonSpecial)
	}
	ttl := minimumResolvedTTL(ttlA, ttlAAAA)
	if ttl <= 0 {
		return resolver.reject(ReasonMalformed)
	}
	minimumTTL := time.Duration(resolver.config.MinimumTTLSeconds) * time.Second
	maximumTTL := time.Duration(resolver.config.MaximumTTLSeconds) * time.Second
	if ttl < minimumTTL {
		ttl = minimumTTL
	}
	if ttl > maximumTTL {
		ttl = maximumTTL
	}
	snapshot := Snapshot{Addresses: append([]netip.Addr(nil), addresses...), ExpiresAt: now.Add(ttl)}
	resolver.cacheMu.Lock()
	resolver.store(hostname, snapshot)
	resolver.cacheMu.Unlock()
	resolver.healthy.Store(true)
	resolver.observe("validated", ReasonNone)
	return snapshot, nil
}

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

func (resolver *Resolver) reject(reason Reason) (Snapshot, error) {
	resolver.healthy.Store(false)
	resolver.observe("rejected", reason)
	return Snapshot{}, &Error{Reason: reason}
}

func (resolver *Resolver) store(hostname string, snapshot Snapshot) {
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

type networkExchanger struct{ timeout time.Duration }

func (exchanger networkExchanger) Exchange(ctx context.Context, request *dns.Msg, server netip.AddrPort, network string) (*dns.Msg, error) {
	client := &dns.Client{Net: network, Timeout: exchanger.timeout, UDPSize: 1232}
	response, _, err := client.ExchangeContext(ctx, request, server.String())
	return response, err
}

func normalizeDNSName(value string) (string, error) {
	value = strings.TrimSuffix(value, ".")
	hostname, err := policy.NormalizeHostname(value)
	if err != nil {
		return "", &Error{Reason: ReasonMalformed}
	}
	return hostname, nil
}

func errorReason(err error) Reason {
	var resolutionErr *Error
	if errors.As(err, &resolutionErr) {
		return resolutionErr.Reason
	}
	return ReasonMalformed
}

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

func minTTL(current, candidate time.Duration) time.Duration {
	if current < 0 || candidate < current {
		return candidate
	}
	return current
}

func minimumResolvedTTL(left, right time.Duration) time.Duration {
	if left < 0 {
		return right
	}
	if right < 0 || left < right {
		return left
	}
	return right
}

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
