package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/dnsresolver"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/mailpolicy"
)

func mailFixture(t *testing.T, protocol, mode string, port int) *mailpolicy.MailActive {
	t.Helper()
	base := loadSTTProfile(t)
	doc := mailpolicy.MailDocument{Schema: mailpolicy.MailSchema, ConfigurationRevision: 1, ConfigurationDigest: strings.Repeat("a", 64), GatewayPolicyDigest: base.Digest(), Destinations: []mailpolicy.MailDestination{{Hostname: "mail.example.test", Port: port, Protocol: protocol, TLSMode: mode, Addresses: []string{"8.8.8.8"}}}}
	raw, _ := json.Marshal(doc)
	active, err := mailpolicy.LoadMail(raw, doc.Digest(), base)
	if err != nil {
		t.Fatal(err)
	}
	return active
}

func TestMailCONNECTAllRegisteredTransportsPreserveOpaqueBytes(t *testing.T) {
	for _, tc := range []struct {
		protocol, mode, greeting string
		port                     int
	}{
		{"smtp", "implicit", "220 fixture\r\n", 465}, {"smtp", "starttls", "220 fixture\r\n", 587},
		{"pop3", "implicit", "+OK fixture\r\n", 995}, {"pop3", "starttls", "+OK fixture\r\n", 110},
		{"imap", "implicit", "* OK fixture\r\n", 993}, {"imap", "starttls", "* OK fixture\r\n", 143},
	} {
		t.Run(fmt.Sprint(tc.port), func(t *testing.T) {
			active := mailFixture(t, tc.protocol, tc.mode, tc.port)
			resolver := &fakeResolver{snapshot: dnsresolver.Snapshot{Addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8")}, ExpiresAt: time.Now().Add(time.Minute)}}
			dialer := &fakeDialer{peers: make(chan net.Conn, 1)}
			server, err := New(context.Background(), "unused", active, resolver, dialer, readyStub(true), newTestMetrics(t))
			if err != nil {
				t.Fatal(err)
			}
			serverSide, client := net.Pipe()
			defer client.Close()
			done := make(chan struct{})
			go func() { defer close(done); defer serverSide.Close(); server.handle(serverSide) }()
			_ = client.SetDeadline(time.Now().Add(2 * time.Second))
			_, err = fmt.Fprintf(client, "CONNECT mail.example.test:%d HTTP/1.1\r\nHost: mail.example.test:%d\r\n\r\n", tc.port, tc.port)
			if err != nil {
				t.Fatal(err)
			}
			reader := bufio.NewReader(client)
			response, err := http.ReadResponse(reader, nil)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != 200 || response.Header.Get("X-Kodex-Egress-Profile") != mailpolicy.MailProfileName || response.Header.Get("X-Kodex-Egress-Digest") != active.Digest() {
				t.Fatal("incorrect mail readback")
			}
			if response.Header.Get("X-Kodex-Egress-Configuration-Revision") != "1" || response.Header.Get("X-Kodex-Egress-Configuration-Digest") != strings.Repeat("a", 64) {
				t.Fatal("mail source readback mismatch")
			}
			hello := gatewayClientHello("mail.example.test")
			if tc.mode == "implicit" {
				go func() { _, _ = client.Write(hello) }()
			}
			var upstream net.Conn
			select {
			case upstream = <-dialer.peers:
			case <-time.After(time.Second):
				t.Fatal("mail dial blocked on ClientHello before server greeting")
			}
			defer upstream.Close()
			_ = upstream.SetDeadline(time.Now().Add(2 * time.Second))
			if tc.mode == "implicit" {
				b := make([]byte, len(hello))
				if _, err := io.ReadFull(upstream, b); err != nil || string(b) != string(hello) {
					t.Fatal("ClientHello modified", err)
				}
			}
			go func() { _, _ = io.WriteString(upstream, tc.greeting) }()
			line, err := reader.ReadString('\n')
			if err != nil || line != tc.greeting {
				t.Fatal("server greeting lost", err)
			}
			if tc.mode == "starttls" {
				go func() { _, _ = client.Write(hello) }()
				b := make([]byte, len(hello))
				if _, err := io.ReadFull(upstream, b); err != nil || string(b) != string(hello) {
					t.Fatal("opaque upgrade bytes modified", err)
				}
			}
			_ = client.Close()
			_ = upstream.Close()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("mail tunnel did not join")
			}
			if len(dialer.targets) != 1 || dialer.targets[0] != netip.AddrPortFrom(netip.MustParseAddr("8.8.8.8"), uint16(tc.port)) {
				t.Fatal("dial escaped exact pin")
			}
		})
	}
}

func TestMailCONNECTRebindingRejectsBeforeDial(t *testing.T) {
	for _, addresses := range [][]netip.Addr{{netip.MustParseAddr("1.1.1.1")}, {netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("10.0.0.1")}} {
		resolver := &fakeResolver{snapshot: dnsresolver.Snapshot{Addresses: addresses, ExpiresAt: time.Now().Add(time.Minute)}}
		dialer := &fakeDialer{peers: make(chan net.Conn, 1)}
		server, err := New(context.Background(), "unused", mailFixture(t, "smtp", "starttls", 587), resolver, dialer, readyStub(true), newTestMetrics(t))
		if err != nil {
			t.Fatal(err)
		}
		exchangeProfileRequest(t, server, "CONNECT mail.example.test:587 HTTP/1.1\r\nHost: mail.example.test:587\r\n\r\n")
		if len(dialer.targets) != 0 {
			t.Fatal("rebound DNS caused external dial")
		}
	}
}

func TestMailReadinessReturnsPinnedSourceAndIgnoresCallerProfile(t *testing.T) {
	for _, ready := range []bool{false, true} {
		resolver, dialer := &fakeResolver{}, &fakeDialer{}
		active := mailFixture(t, "smtp", "starttls", 587)
		server, err := New(t.Context(), "unused", active, resolver, dialer, readyStub(ready), newTestMetrics(t))
		if err != nil {
			t.Fatal(err)
		}
		response := exchangeProfileRequest(t, server, "GET /readyz HTTP/1.1\r\nHost: egress-gateway.kodex-system.svc:8082\r\nX-Kodex-Egress-Profile: default\r\nX-Kodex-Egress-Configuration-Revision: 999\r\n\r\n")
		status := http.StatusServiceUnavailable
		if ready {
			status = http.StatusNoContent
		}
		if response.StatusCode != status || response.Header.Get("X-Kodex-Egress-Profile") != mailpolicy.MailProfileName || response.Header.Get("X-Kodex-Egress-Configuration-Revision") != "1" || response.Header.Get("X-Kodex-Egress-Configuration-Digest") != strings.Repeat("a", 64) {
			t.Fatal("readiness source substitution")
		}
		if resolver.calls != 0 || len(dialer.targets) != 0 {
			t.Fatal("readiness caused external dial")
		}
	}
}

func TestMailImplicitSNIRejectsBeforeDNS(t *testing.T) {
	resolver, dialer := &fakeResolver{}, &fakeDialer{}
	server, err := New(t.Context(), "unused", mailFixture(t, "smtp", "implicit", 465), resolver, dialer, readyStub(true), newTestMetrics(t))
	if err != nil {
		t.Fatal(err)
	}
	serverSide, client := net.Pipe()
	defer client.Close()
	done := make(chan struct{})
	go func() { defer close(done); defer serverSide.Close(); server.handle(serverSide) }()
	_ = client.SetDeadline(time.Now().Add(time.Second))
	_, _ = io.WriteString(client, "CONNECT mail.example.test:465 HTTP/1.1\r\nHost: mail.example.test:465\r\n\r\n")
	if _, err := http.ReadResponse(bufio.NewReader(client), nil); err != nil {
		t.Fatal(err)
	}
	_, _ = client.Write(gatewayClientHello("other.example.test"))
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SNI rejection did not join")
	}
	if resolver.calls != 0 || len(dialer.targets) != 0 {
		t.Fatal("incorrect SNI crossed DNS boundary")
	}
}
