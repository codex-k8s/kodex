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

func TestConfigFromRuntimeSettingsMapsAllFields(t *testing.T) {
	t.Parallel()

	settings := RuntimeSettings{
		MaxInFlight:          16,
		MaxConcurrentStreams: 32,
		UnaryTimeout:         2 * time.Second,
		KeepaliveTime:        3 * time.Minute,
		KeepaliveTimeout:     4 * time.Second,
		KeepaliveMinTime:     5 * time.Second,
		PermitWithoutStream:  true,
		MaxRecvMessageBytes:  2048,
		MaxSendMessageBytes:  4096,
		AuthRequired:         true,
	}
	cfg := ConfigFromRuntimeSettings(settings)
	if cfg.MaxInFlight != settings.MaxInFlight ||
		cfg.MaxConcurrentStreams != settings.MaxConcurrentStreams ||
		cfg.UnaryTimeout != settings.UnaryTimeout ||
		cfg.KeepaliveTime != settings.KeepaliveTime ||
		cfg.KeepaliveTimeout != settings.KeepaliveTimeout ||
		cfg.KeepaliveMinTime != settings.KeepaliveMinTime ||
		cfg.PermitWithoutStream != settings.PermitWithoutStream ||
		cfg.MaxRecvMessageBytes != settings.MaxRecvMessageBytes ||
		cfg.MaxSendMessageBytes != settings.MaxSendMessageBytes ||
		cfg.AuthRequired != settings.AuthRequired {
		t.Fatalf("ConfigFromRuntimeSettings() = %+v, want values from %+v", cfg, settings)
	}
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
