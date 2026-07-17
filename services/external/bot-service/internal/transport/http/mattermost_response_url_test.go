package http

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
)

type fakeMattermostResolver struct {
	mu        sync.Mutex
	addresses [][]net.IPAddr
	calls     int
}

func (resolver *fakeMattermostResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	resolver.calls++
	if len(resolver.addresses) == 0 {
		return nil, errors.New("DNS result is absent")
	}
	index := resolver.calls - 1
	if index >= len(resolver.addresses) {
		index = len(resolver.addresses) - 1
	}
	return resolver.addresses[index], nil
}

type pipeMattermostDialer struct {
	mu        sync.Mutex
	addresses []string
	status    int
	headers   map[string]string
	delivered chan []byte
}

func (dialer *pipeMattermostDialer) DialContext(_ context.Context, _ string, address string) (net.Conn, error) {
	dialer.mu.Lock()
	dialer.addresses = append(dialer.addresses, address)
	dialer.mu.Unlock()
	client, server := net.Pipe()
	go func() {
		defer server.Close()
		request, err := http.ReadRequest(bufio.NewReader(server))
		if err != nil {
			return
		}
		body, _ := io.ReadAll(request.Body)
		_ = request.Body.Close()
		if dialer.delivered != nil {
			dialer.delivered <- body
		}
		status := dialer.status
		if status == 0 {
			status = http.StatusOK
		}
		_, _ = fmt.Fprintf(server, "HTTP/1.1 %d %s\r\n", status, http.StatusText(status))
		for key, value := range dialer.headers {
			_, _ = fmt.Fprintf(server, "%s: %s\r\n", key, value)
		}
		_, _ = fmt.Fprint(server, "Content-Length: 0\r\nConnection: close\r\n\r\n")
	}()
	return client, nil
}

func TestMattermostResponseURLOriginMatrix(t *testing.T) {
	client := newMattermostResponseClient("https://mattermost.example.com", "http://mattermost.mattermost.svc.cluster.local:8065", nil, nil)
	tests := []string{
		"https://attacker.example/callback",
		"https://127.0.0.1/callback",
		"http://mattermost.example.com/callback",
		"https://mattermost.example.com:444/callback",
		"https://user@mattermost.example.com/callback",
		"https://mattermost.example.com/callback#fragment",
	}
	for _, rawURL := range tests {
		t.Run(rawURL, func(t *testing.T) {
			if _, _, err := client.validateURL(rawURL); !errors.Is(err, ErrMattermostResponseURLDenied) {
				t.Fatalf("validateURL(%q) error = %v", rawURL, err)
			}
		})
	}
	if _, _, err := client.validateURL("https://mattermost.example.com/hooks/response-id"); err != nil {
		t.Fatalf("expected origin rejected: %v", err)
	}
}

func TestMattermostResponseIPAddressMatrix(t *testing.T) {
	tests := []struct {
		name         string
		address      string
		allowPrivate bool
		want         bool
	}{
		{name: "public", address: "8.8.8.8", want: true},
		{name: "documentation range", address: "203.0.113.10", want: false},
		{name: "private external", address: "10.1.2.3", want: false},
		{name: "private internal allowlist", address: "10.1.2.3", allowPrivate: true, want: true},
		{name: "public outside internal service range", address: "8.8.8.8", allowPrivate: true, want: false},
		{name: "carrier grade nat", address: "100.64.0.1", allowPrivate: true, want: false},
		{name: "loopback", address: "127.0.0.1", allowPrivate: true, want: false},
		{name: "link local", address: "169.254.10.20", allowPrivate: true, want: false},
		{name: "metadata aws", address: "169.254.169.254", allowPrivate: true, want: false},
		{name: "metadata ecs", address: "169.254.170.2", allowPrivate: true, want: false},
		{name: "metadata alibaba", address: "100.100.100.200", allowPrivate: true, want: false},
		{name: "unspecified", address: "0.0.0.0", allowPrivate: true, want: false},
		{name: "ipv6 loopback", address: "::1", allowPrivate: true, want: false},
		{name: "ipv6 link local", address: "fe80::1", allowPrivate: true, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := allowedMattermostIP(net.ParseIP(test.address), test.allowPrivate); got != test.want {
				t.Fatalf("allowedMattermostIP(%s, %t) = %t, want %t", test.address, test.allowPrivate, got, test.want)
			}
		})
	}
}

func TestMattermostResponseArbitraryOriginCreatesNoOutboundConnection(t *testing.T) {
	resolver := &fakeMattermostResolver{addresses: [][]net.IPAddr{{{IP: net.ParseIP("8.8.8.8")}}}}
	dialer := &pipeMattermostDialer{}
	client := newMattermostResponseClient("https://mattermost.example.com", "", resolver, dialer)
	err := client.PostJSON(context.Background(), "https://attacker.example/hooks/value", []byte(`{}`))
	if !errors.Is(err, ErrMattermostResponseURLDenied) {
		t.Fatalf("PostJSON() error = %v", err)
	}
	if resolver.calls != 0 || len(dialer.addresses) != 0 {
		t.Fatalf("arbitrary origin caused outbound work: DNS=%d dial=%#v", resolver.calls, dialer.addresses)
	}
}

func TestMattermostResponsePinsSingleDNSResolution(t *testing.T) {
	resolver := &fakeMattermostResolver{addresses: [][]net.IPAddr{
		{{IP: net.ParseIP("10.20.30.40")}},
		{{IP: net.ParseIP("169.254.169.254")}},
	}}
	dialer := &pipeMattermostDialer{}
	client := newMattermostResponseClient("", "http://mattermost.mattermost.svc.cluster.local:8065", resolver, dialer)
	if err := client.PostJSON(context.Background(), "http://mattermost.mattermost.svc.cluster.local:8065/hooks/value", []byte(`{"ok":true}`)); err != nil {
		t.Fatalf("PostJSON() error = %v", err)
	}
	if resolver.calls != 1 {
		t.Fatalf("DNS calls = %d, want 1", resolver.calls)
	}
	if len(dialer.addresses) != 1 || dialer.addresses[0] != "10.20.30.40:8065" {
		t.Fatalf("dialed addresses = %#v", dialer.addresses)
	}
}

func TestMattermostResponseDeniesPrivateExternalResolution(t *testing.T) {
	resolver := &fakeMattermostResolver{addresses: [][]net.IPAddr{{{IP: net.ParseIP("10.20.30.40")}}}}
	dialer := &pipeMattermostDialer{}
	client := newMattermostResponseClient("https://mattermost.example.com", "", resolver, dialer)
	err := client.PostJSON(context.Background(), "https://mattermost.example.com/hooks/value", []byte(`{}`))
	if !errors.Is(err, ErrMattermostResponseURLDenied) {
		t.Fatalf("PostJSON() error = %v", err)
	}
	if len(dialer.addresses) != 0 {
		t.Fatalf("unexpected outbound dial: %#v", dialer.addresses)
	}
}

func TestMattermostResponseDeniesRedirectWithoutFollowing(t *testing.T) {
	resolver := &fakeMattermostResolver{addresses: [][]net.IPAddr{{{IP: net.ParseIP("10.20.30.40")}}}}
	dialer := &pipeMattermostDialer{status: http.StatusFound, headers: map[string]string{"Location": "http://169.254.169.254/latest/meta-data"}}
	client := newMattermostResponseClient("", "http://mattermost.mattermost.svc.cluster.local:8065", resolver, dialer)
	err := client.PostJSON(context.Background(), "http://mattermost.mattermost.svc.cluster.local:8065/hooks/value", []byte(`{}`))
	if err == nil {
		t.Fatal("PostJSON() unexpectedly followed or accepted redirect")
	}
	if resolver.calls != 1 || len(dialer.addresses) != 1 {
		t.Fatalf("redirect caused extra resolution or dial: DNS=%d dial=%#v", resolver.calls, dialer.addresses)
	}
}
