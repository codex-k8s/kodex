package dnsresolver

import (
	"errors"
	"net/netip"

	shared "github.com/codex-k8s/kodex/libs/go/mailpolicy"
)

// ValidateAddresses rejects an empty or mixed public/special-purpose DNS snapshot.
// It preserves the resolver's closed reason categories while delegating the
// address boundary to the same policy used by control-plane network pins.
// RU: ValidateAddresses отклоняет пустой DNS-снимок и смесь публичных адресов
// со специальными. Метод сохраняет закрытые категории причин resolver и
// использует ту же проверку адресов, что и сетевые pins control plane.
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
