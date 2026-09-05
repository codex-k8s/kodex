package dnsresolver

import (
	"errors"
	shared "github.com/codex-k8s/kodex/libs/go/mailpolicy"
	"net/netip"
)

// ValidateAddresses сохраняет закрытые reason metrics общего resolver,
// используя ту же public-address границу, что CP producer сетевых pins.
func ValidateAddresses(addresses []netip.Addr) error {
	err := shared.ValidateAddresses(addresses)
	if errors.Is(err, shared.ErrEmptyAddresses) {
		return &Error{Reason: ReasonEmpty}
	}
	if err != nil {
		return &Error{Reason: ReasonSpecial}
	}
	return nil
}
