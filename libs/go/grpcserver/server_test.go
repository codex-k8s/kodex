package grpcserver

import (
	"testing"
	"time"
)

func TestNewServerRequiresAuthenticatorWhenAuthEnabled(t *testing.T) {
	t.Parallel()

	_, err := NewServer(validConfig(), Dependencies{})
	if err == nil {
		t.Fatal("NewServer() err is nil, want authenticator error")
	}
}

func TestNewServerBuildsWithSharedRuntime(t *testing.T) {
	t.Parallel()

	server, err := NewServer(validConfig(), Dependencies{
		Authenticator: NewSharedTokenAuthenticator("token"),
	})
	if err != nil {
		t.Fatalf("NewServer(): %v", err)
	}
	server.Stop()
}

func validConfig() Config {
	return Config{
		MaxInFlight:          8,
		MaxConcurrentStreams: 8,
		UnaryTimeout:         time.Second,
		KeepaliveTime:        time.Minute,
		KeepaliveTimeout:     20 * time.Second,
		KeepaliveMinTime:     30 * time.Second,
		MaxRecvMessageBytes:  1024,
		MaxSendMessageBytes:  1024,
		AuthRequired:         true,
	}
}
