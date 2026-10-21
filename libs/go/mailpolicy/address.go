package mailpolicy

import (
	"errors"
	"net/netip"
)

var (
	// ErrEmptyAddresses rejects DNS snapshots without any resolved destination.
	// RU: ErrEmptyAddresses отклоняет снимок DNS без единого разрешённого адреса назначения.
	ErrEmptyAddresses = errors.New("DNS snapshot is empty")
	// ErrSpecialAddress rejects a snapshot containing a private, reserved or otherwise unsafe address.
	// RU: ErrSpecialAddress отклоняет снимок с частным, зарезервированным либо иным небезопасным адресом.
	ErrSpecialAddress = errors.New("DNS snapshot contains a prohibited address")
)

// prohibitedPrefixes enumerates ranges that must never become mail egress pins,
// including special-use IPv4 and IPv6 ranges not covered by netip predicates.
// RU: prohibitedPrefixes перечисляет диапазоны, недопустимые для почтовых сетевых
// привязок, включая специальные IPv4/IPv6, не охваченные проверками netip.
var prohibitedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.31.196.0/24"),
	netip.MustParsePrefix("192.52.193.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("192.175.48.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("::ffff:0:0/96"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("100:0:0:1::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("2620:4f:8000::/48"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("5f00::/16"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("fec0::/10"),
}

// allocatedIPv6GlobalUnicast bounds IPv6 candidates to the global unicast space.
// RU: allocatedIPv6GlobalUnicast ограничивает IPv6-кандидаты пространством глобальной одноадресной маршрутизации.
var allocatedIPv6GlobalUnicast = netip.MustParsePrefix("2000::/3")

// ValidateAddresses rejects the entire DNS snapshot when it is empty or any
// address is invalid, private, reserved, multicast or outside public IPv6.
// RU: ValidateAddresses отклоняет весь снимок DNS, если он пуст либо хотя бы
// один адрес некорректен, частный, зарезервирован, групповой или не публичный IPv6.
func ValidateAddresses(addresses []netip.Addr) error {
	if len(addresses) == 0 {
		return ErrEmptyAddresses
	}
	for _, address := range addresses {
		if !address.IsValid() || address.Is4In6() || address.IsUnspecified() || address.IsLoopback() ||
			address.IsPrivate() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() {
			return ErrSpecialAddress
		}
		if address.Is6() && !allocatedIPv6GlobalUnicast.Contains(address) {
			return ErrSpecialAddress
		}
		for _, prefix := range prohibitedPrefixes {
			if prefix.Contains(address) {
				return ErrSpecialAddress
			}
		}
	}
	return nil
}
