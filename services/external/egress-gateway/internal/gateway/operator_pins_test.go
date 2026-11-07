package gateway

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"
)

// operatorPinsDialer records literal targets and deadlines without resolving names or using a network.
// operatorPinsDialer сохраняет literal targets и deadlines без разрешения имён либо сети.
type operatorPinsDialer struct {
	targets   []netip.AddrPort
	deadlines []time.Time
	wait      bool
	peer      net.Conn
}

// DialContext blocks until cancellation or succeeds on the second synthetic IPv6 target.
// DialContext ждёт отмены либо завершается на второй synthetic IPv6 цели.
func (d *operatorPinsDialer) DialContext(ctx context.Context, target netip.AddrPort) (net.Conn, error) {
	d.targets = append(d.targets, target)
	deadline, _ := ctx.Deadline()
	d.deadlines = append(d.deadlines, deadline)
	if d.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if len(d.targets) == 1 {
		return nil, errors.New("synthetic first address unavailable")
	}
	connection, peer := net.Pipe()
	d.peer = peer
	return connection, nil
}

// TestOperatorPinsShareOneIPv4IPv6DialBudget proves deterministic literal selection, one deadline,
// bounded timeout and zero dial after pre-cancellation; retries cannot reset the total dial budget.
// TestOperatorPinsShareOneIPv4IPv6DialBudget проверяет deterministic literal selection, общий
// deadline, bounded timeout и ноль dial после pre-cancellation; retries не сбрасывают общий бюджет.
func TestOperatorPinsShareOneIPv4IPv6DialBudget(t *testing.T) {
	pins := []string{"10.77.0.4/32", "fd77::4/128"}
	dialer := &operatorPinsDialer{}
	server := &Server{dialer: dialer}
	connection, err := server.dialOperatorPins(t.Context(), pins, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	defer dialer.peer.Close()
	if len(dialer.targets) != 2 || dialer.targets[0] != netip.MustParseAddrPort("10.77.0.4:443") || dialer.targets[1] != netip.MustParseAddrPort("[fd77::4]:443") || dialer.deadlines[0].IsZero() || !dialer.deadlines[0].Equal(dialer.deadlines[1]) {
		t.Fatal("operator pins changed address, port or total deadline")
	}
	blocked := &operatorPinsDialer{wait: true}
	server.dialer = blocked
	started := time.Now()
	if connection, err := server.dialOperatorPins(t.Context(), pins, 10*time.Millisecond); err == nil || connection != nil || len(blocked.targets) != 1 || time.Since(started) > time.Second {
		t.Fatal("operator dial did not respect one bounded timeout")
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	untouched := &operatorPinsDialer{}
	server.dialer = untouched
	if connection, err := server.dialOperatorPins(cancelled, pins, time.Second); err == nil || connection != nil || len(untouched.targets) != 0 {
		t.Fatal("pre-cancelled operator route attempted a dial")
	}
}
