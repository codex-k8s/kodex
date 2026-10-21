package integrationegresspolicy

import (
	"context"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// resolverFixture returns a fixed address set with a short future expiry for
// producer tests; it is not a network resolver.
// RU: resolverFixture возвращает фиксированные адреса с коротким будущим
// сроком для тестов producer и не обращается к сети.
type resolverFixture struct{ addresses []netip.Addr }

// Resolve returns a fresh fixture snapshot for the requested hostname without
// using DNS or deriving authority from the hostname argument.
// RU: Resolve возвращает свежий тестовый снимок для имени без DNS-запроса и
// без вывода полномочий из аргумента имени.
func (fixture resolverFixture) Resolve(context.Context, string) (Snapshot, error) {
	return Snapshot{Addresses: fixture.addresses, ExpiresAt: time.Now().Add(time.Minute)}, nil
}

// TestProduceBindsExactOriginsAndRejectsMixedDNS checks sorted exact origins,
// canonical public pins and fail-closed handling of duplicate, wildcard,
// literal, uppercase or mixed-private DNS input.
// RU: TestProduceBindsExactOriginsAndRejectsMixedDNS проверяет упорядоченные
// точные origins, публичные pins и закрытый отказ для повторов, wildcard,
// IP-литерала, прописных букв и смешанного частного DNS.
func TestProduceBindsExactOriginsAndRejectsMixedDNS(t *testing.T) {
	resolver := resolverFixture{addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("1.1.1.1")}}
	document, err := Produce(t.Context(), []string{"api.example.test", "another.example.test"}, 2, strings.Repeat("a", 64), resolver)
	if err != nil || document.Validate() != nil || len(document.Destinations) != 2 ||
		document.Destinations[0].Hostname != "another.example.test" || document.Destinations[0].Addresses[0] != "1.1.1.1" {
		t.Fatalf("valid exact origin projection failed: %v", err)
	}
	for name, hosts := range map[string][]string{
		"duplicate": {"api.example.test", "api.example.test"},
		"wildcard":  {"*.example.test"},
		"literal":   {"8.8.8.8"},
		"uppercase": {"Api.example.test"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Produce(t.Context(), hosts, 2, strings.Repeat("a", 64), resolver); err == nil {
				t.Fatal("invalid origin accepted")
			}
		})
	}
	resolver.addresses = append(resolver.addresses, netip.MustParseAddr("10.0.0.1"))
	if _, err := Produce(t.Context(), []string{"api.example.test"}, 2, strings.Repeat("a", 64), resolver); err == nil {
		t.Fatal("mixed private DNS snapshot accepted")
	}
	document.Destinations[0].Addresses = []string{"10.0.0.1"}
	if document.Validate() == nil {
		t.Fatal("private pin accepted by consumer validation")
	}
}

// TestEmptyFirstRunProjectionDoesNotGrantDestinations permits an explicit
// empty first-run projection but rejects a forged source digest.
// RU: TestEmptyFirstRunProjectionDoesNotGrantDestinations допускает явную
// пустую начальную проекцию и отклоняет подложный дайджест источника.
func TestEmptyFirstRunProjectionDoesNotGrantDestinations(t *testing.T) {
	document, err := Produce(t.Context(), nil, 1, strings.Repeat("b", 64), resolverFixture{})
	if err != nil || document.Validate() != nil || len(document.Destinations) != 0 ||
		document.SourceDigest != SourceDigest(nil) || document.Digest() == "" {
		t.Fatalf("empty first-run projection is invalid: %v", err)
	}
	document.SourceDigest = strings.Repeat("c", 64)
	if document.Validate() == nil {
		t.Fatal("forged source digest accepted")
	}
}
