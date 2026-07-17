package http

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var ErrMattermostResponseURLDenied = errors.New("mattermost response URL denied")

type mattermostDNSResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

type mattermostContextDialer interface {
	DialContext(ctx context.Context, network string, address string) (net.Conn, error)
}

type mattermostAllowedOrigin struct {
	scheme      string
	hostname    string
	port        string
	privateOnly bool
}

type mattermostResponseClient struct {
	origins  []mattermostAllowedOrigin
	resolver mattermostDNSResolver
	dialer   mattermostContextDialer
}

type mattermostResponseTarget struct {
	url    *url.URL
	origin mattermostAllowedOrigin
	pinned []net.IP
}

func newMattermostResponseClient(siteURL string, internalURL string, resolver mattermostDNSResolver, dialer mattermostContextDialer) *mattermostResponseClient {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	if dialer == nil {
		dialer = &net.Dialer{Timeout: 5 * time.Second}
	}
	client := &mattermostResponseClient{resolver: resolver, dialer: dialer}
	for _, item := range []struct {
		raw         string
		privateOnly bool
	}{{siteURL, false}, {internalURL, true}} {
		origin, ok := parseMattermostAllowedOrigin(item.raw, item.privateOnly)
		if ok && !containsMattermostOrigin(client.origins, origin) {
			client.origins = append(client.origins, origin)
		}
	}
	return client
}

func (client *mattermostResponseClient) PostJSON(ctx context.Context, rawURL string, body []byte) error {
	target, err := client.Prepare(ctx, rawURL)
	if err != nil {
		return err
	}
	return client.PostPreparedJSON(ctx, target, body)
}

func (client *mattermostResponseClient) Prepare(ctx context.Context, rawURL string) (*mattermostResponseTarget, error) {
	if client == nil || len(client.origins) == 0 {
		return nil, ErrMattermostResponseURLDenied
	}
	target, origin, err := client.validateURL(rawURL)
	if err != nil {
		return nil, err
	}
	addresses, err := client.resolver.LookupIPAddr(ctx, origin.hostname)
	if err != nil || len(addresses) == 0 {
		return nil, fmt.Errorf("%w: DNS resolution failed", ErrMattermostResponseURLDenied)
	}
	pinned := make([]net.IP, 0, len(addresses))
	for _, address := range addresses {
		ip := address.IP
		if !allowedMattermostIP(ip, origin.privateOnly) {
			return nil, fmt.Errorf("%w: destination address is not allowed", ErrMattermostResponseURLDenied)
		}
		pinned = append(pinned, append(net.IP(nil), ip...))
	}
	return &mattermostResponseTarget{url: target, origin: origin, pinned: pinned}, nil
}

func (client *mattermostResponseClient) PostPreparedJSON(ctx context.Context, target *mattermostResponseTarget, body []byte) error {
	if client == nil || target == nil || target.url == nil || len(target.pinned) == 0 {
		return ErrMattermostResponseURLDenied
	}
	transport := &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12, ServerName: target.origin.hostname},
	}
	transport.DialContext = func(dialCtx context.Context, network string, _ string) (net.Conn, error) {
		var lastErr error
		for _, ip := range target.pinned {
			connection, dialErr := client.dialer.DialContext(dialCtx, network, net.JoinHostPort(ip.String(), target.origin.port))
			if dialErr == nil {
				return connection, nil
			}
			lastErr = dialErr
		}
		return nil, lastErr
	}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target.url.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create Mattermost response request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	response, err := httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("post Mattermost response: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("mattermost response returned status %d", response.StatusCode)
	}
	return nil
}

func (client *mattermostResponseClient) validateURL(rawURL string) (*url.URL, mattermostAllowedOrigin, error) {
	target, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || target.Scheme == "" || target.Host == "" || target.User != nil || target.Fragment != "" {
		return nil, mattermostAllowedOrigin{}, ErrMattermostResponseURLDenied
	}
	if net.ParseIP(target.Hostname()) != nil {
		return nil, mattermostAllowedOrigin{}, fmt.Errorf("%w: IP literal is forbidden", ErrMattermostResponseURLDenied)
	}
	port, ok := normalizedURLPort(target)
	if !ok {
		return nil, mattermostAllowedOrigin{}, ErrMattermostResponseURLDenied
	}
	for _, origin := range client.origins {
		if strings.EqualFold(target.Scheme, origin.scheme) && strings.EqualFold(target.Hostname(), origin.hostname) && port == origin.port {
			if target.Scheme != "https" && !origin.privateOnly {
				return nil, mattermostAllowedOrigin{}, ErrMattermostResponseURLDenied
			}
			return target, origin, nil
		}
	}
	return nil, mattermostAllowedOrigin{}, ErrMattermostResponseURLDenied
}

func parseMattermostAllowedOrigin(rawURL string, privateOnly bool) (mattermostAllowedOrigin, bool) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || net.ParseIP(parsed.Hostname()) != nil {
		return mattermostAllowedOrigin{}, false
	}
	port, ok := normalizedURLPort(parsed)
	if !ok || (parsed.Scheme != "https" && !(privateOnly && parsed.Scheme == "http")) {
		return mattermostAllowedOrigin{}, false
	}
	return mattermostAllowedOrigin{
		scheme:      strings.ToLower(parsed.Scheme),
		hostname:    strings.ToLower(parsed.Hostname()),
		port:        port,
		privateOnly: privateOnly,
	}, true
}

func normalizedURLPort(value *url.URL) (string, bool) {
	port := value.Port()
	if port == "" {
		switch strings.ToLower(value.Scheme) {
		case "https":
			return "443", true
		case "http":
			return "80", true
		default:
			return "", false
		}
	}
	parsed, err := strconv.Atoi(port)
	if err != nil || parsed < 1 || parsed > 65535 {
		return "", false
	}
	return strconv.Itoa(parsed), true
}

func allowedMattermostIP(ip net.IP, privateOnly bool) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	address, ok := netip.AddrFromSlice(ip)
	if !ok || isDeniedMattermostAddress(address.Unmap()) {
		return false
	}
	if privateOnly {
		return ip.IsPrivate()
	}
	return !ip.IsPrivate()
}

var deniedMattermostPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("100.100.100.200/32"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("2001:db8::/32"),
}

func isDeniedMattermostAddress(address netip.Addr) bool {
	for _, prefix := range deniedMattermostPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func containsMattermostOrigin(origins []mattermostAllowedOrigin, candidate mattermostAllowedOrigin) bool {
	for _, origin := range origins {
		if origin.scheme == candidate.scheme && origin.hostname == candidate.hostname && origin.port == candidate.port {
			return true
		}
	}
	return false
}
