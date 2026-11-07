package runtimecontract

import "net/netip"

// ValidOperatorAddressPins checks only the shape of an explicit deployment-owned literal route:
// bounded unique canonical /32 or /128 global-unicast addresses, without ambient DNS or broad CIDRs.
// It does not authorize a route; callers must bind an operator policy and may reject further ranges.
// ValidOperatorAddressPins проверяет только форму явного маршрута установки: уникальные canonical
// /32 либо /128 unicast addresses без DNS и широких CIDR. Полномочия она не выдаёт; caller обязан
// закрепить operator policy и при необходимости запретить дополнительные специальные диапазоны.
func ValidOperatorAddressPins(values []string) bool {
	if len(values) < 1 || len(values) > 32 {
		return false
	}
	addresses := map[netip.Addr]bool{}
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || prefix.String() != value || prefix.Bits() != prefix.Addr().BitLen() ||
			!prefix.Addr().IsGlobalUnicast() || prefix.Addr().IsLoopback() ||
			prefix.Addr().IsLinkLocalUnicast() || prefix.Addr().Is4In6() || addresses[prefix.Addr()] {
			return false
		}
		addresses[prefix.Addr()] = true
	}
	return true
}
