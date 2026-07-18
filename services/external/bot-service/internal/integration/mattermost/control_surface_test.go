package mattermost

import (
	"net/http"
	"testing"
	"time"
)

func TestControlSurfaceUsesBoundedHTTPTransportForEveryToken(t *testing.T) {
	config := HTTPClientConfig{
		Timeout:               7 * time.Second,
		DialTimeout:           2 * time.Second,
		TLSHandshakeTimeout:   3 * time.Second,
		ResponseHeaderTimeout: 4 * time.Second,
		IdleConnTimeout:       45 * time.Second,
	}
	surface := NewControlSurfaceWithHTTPConfig("https://mattermost.example", "bot-token", "admin-token", config)
	if surface.client.HTTPClient.Timeout != config.Timeout || surface.adminClient.HTTPClient.Timeout != config.Timeout {
		t.Fatal("основной или административный Client4 не получил общий HTTP timeout")
	}
	transport, ok := surface.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("HTTP transport = %T", surface.httpClient.Transport)
	}
	if transport.TLSHandshakeTimeout != config.TLSHandshakeTimeout ||
		transport.ResponseHeaderTimeout != config.ResponseHeaderTimeout ||
		transport.IdleConnTimeout != config.IdleConnTimeout || transport.MaxConnsPerHost <= 0 {
		t.Fatalf("неполные transport bounds: %#v", transport)
	}
	if transport.Proxy != nil {
		t.Fatal("Mattermost transport не должен передавать bearer token через environment proxy")
	}
	tokenClient := surface.clientWithToken("role-token")
	if tokenClient.HTTPClient != surface.httpClient || tokenClient.HTTPClient.Timeout == 0 {
		t.Fatal("token-specific Client4 обошёл общий bounded HTTP client")
	}
}
