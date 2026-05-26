package app

import (
	"strings"
	"testing"
	"time"
)

func TestLoadConfigDefaultsWhenAuthDisabled(t *testing.T) {
	t.Setenv("KODEX_INTERACTION_HUB_GRPC_AUTH_REQUIRED", "false")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig(): %v", err)
	}
	if cfg.HTTPAddr != ":8080" || cfg.GRPCAddr != ":9090" {
		t.Fatalf("unexpected listen addresses: http=%q grpc=%q", cfg.HTTPAddr, cfg.GRPCAddr)
	}
	if cfg.GRPC.MaxInFlight != 128 || cfg.GRPC.UnaryTimeout != 30*time.Second {
		t.Fatalf("unexpected grpc defaults: %+v", cfg.GRPC)
	}
}

func TestConfigRequiresAuthTokenWhenAuthEnabled(t *testing.T) {
	cfg := Config{
		HTTPAddr: ":8080",
		GRPCAddr: ":9090",
		GRPC: InteractionGRPCConfig{
			AuthRequired:         true,
			MaxInFlight:          1,
			MaxConcurrentStreams: 1,
			UnaryTimeout:         time.Second,
			KeepaliveTime:        time.Second,
			KeepaliveTimeout:     time.Second,
			KeepaliveMinTime:     time.Second,
			MaxRecvMessageBytes:  1,
			MaxSendMessageBytes:  1,
		},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "KODEX_INTERACTION_HUB_GRPC_AUTH_TOKEN") {
		t.Fatalf("Validate() err = %v, want auth token error", err)
	}
}

func TestConfigRejectsInvalidGRPCBounds(t *testing.T) {
	cfg := Config{
		HTTPAddr: ":8080",
		GRPCAddr: ":9090",
		GRPC: InteractionGRPCConfig{
			AuthRequired:         false,
			MaxInFlight:          0,
			MaxConcurrentStreams: 1,
			UnaryTimeout:         time.Second,
			KeepaliveTime:        time.Second,
			KeepaliveTimeout:     time.Second,
			KeepaliveMinTime:     time.Second,
			MaxRecvMessageBytes:  1,
			MaxSendMessageBytes:  1,
		},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "KODEX_INTERACTION_HUB_GRPC_MAX_IN_FLIGHT") {
		t.Fatalf("Validate() err = %v, want max in flight error", err)
	}
}
