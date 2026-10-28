package errorclass

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"
)

// TestNetworkClosedClasses pins the extracted email classifier's precedence and private-data fallback.
// It exercises only typed standard-library errors and never opens a socket or formats their details.
// TestNetworkClosedClasses закрепляет приоритет и fallback извлечённого email-классификатора без приватных данных.
// Проверяются только типизированные ошибки стандартной библиотеки без открытия socket и форматирования деталей.
func TestNetworkClosedClasses(t *testing.T) {
	t.Parallel()
	const sentinel = "SENTINEL_PRIVATE_NETWORK_DETAIL"
	for _, scenario := range []struct {
		name, class string
		cause       error
	}{
		{"nil", "unknown", nil},
		{"text", "unknown", errors.New(sentinel)},
		{"refused", "connection_refused", fmt.Errorf("%w", syscall.ECONNREFUSED)},
		{"reset", "connection_reset", &os.SyscallError{Syscall: sentinel, Err: syscall.ECONNRESET}},
		{"host", "host_unreachable", syscall.EHOSTUNREACH},
		{"network", "network_unreachable", syscall.ENETUNREACH},
		{"timeout", "network_timeout", syscall.ETIMEDOUT},
		{"dns-not-found", "dns_not_found", &net.DNSError{Name: sentinel, Err: sentinel, IsNotFound: true, IsTimeout: true, IsTemporary: true}},
		{"dns-timeout", "dns_timeout", &net.DNSError{Name: sentinel, Err: sentinel, IsTimeout: true, IsTemporary: true}},
		{"dns-temporary", "dns_temporary", &net.DNSError{Name: sentinel, Err: sentinel, IsTemporary: true}},
		{"dns", "dns", &net.DNSError{Name: sentinel, Err: sentinel}},
		{"net-error", "network", &net.OpError{Op: sentinel, Net: sentinel, Err: errors.New(sentinel)}},
		{"dns-before-syscall", "dns_not_found", errors.Join(syscall.ECONNREFUSED, &net.DNSError{IsNotFound: true})},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			if Network(scenario.cause) != scenario.class {
				t.Fatal("unexpected closed network class")
			}
		})
	}
}
