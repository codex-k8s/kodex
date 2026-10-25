package errorclass

import (
	"errors"
	"net"
	"syscall"
)

// Network maps typed DNS, syscall and network errors to closed labels without reading error text.
// Callers establish TLS/configuration/database precedence and retry policy; unknown causes remain unknown.
// Network переводит типизированные DNS, syscall и network ошибки в закрытые метки без чтения текста ошибки.
// Вызывающий задаёт приоритет TLS/configuration/database и политику повторов; неизвестная причина остаётся unknown.
func Network(err error) string {
	var dns *net.DNSError
	if errors.As(err, &dns) {
		if dns.IsNotFound {
			return "dns_not_found"
		}
		if dns.IsTimeout {
			return "dns_timeout"
		}
		if dns.IsTemporary {
			return "dns_temporary"
		}
		return "dns"
	}
	switch {
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection_refused"
	case errors.Is(err, syscall.ECONNRESET):
		return "connection_reset"
	case errors.Is(err, syscall.EHOSTUNREACH):
		return "host_unreachable"
	case errors.Is(err, syscall.ENETUNREACH):
		return "network_unreachable"
	case errors.Is(err, syscall.ETIMEDOUT):
		return "network_timeout"
	}
	var network net.Error
	if errors.As(err, &network) {
		return "network"
	}
	return "unknown"
}
