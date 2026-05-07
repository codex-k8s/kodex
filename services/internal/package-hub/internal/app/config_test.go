package app

import (
	"testing"
	"time"
)

func TestLoadConfigAllowsMissingConditionalEnvWhenAuthDisabled(t *testing.T) {
	t.Setenv("KODEX_PACKAGE_HUB_GRPC_AUTH_REQUIRED", "false")
	t.Setenv("KODEX_PACKAGE_HUB_GRPC_AUTH_TOKEN", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig(): %v", err)
	}
	if cfg.GRPCAuthRequired {
		t.Fatal("GRPCAuthRequired = true, want false")
	}
}

func TestValidateRequiresGRPCAuthTokenWhenAuthEnabled(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.GRPCAuthToken = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() err = nil, want gRPC auth token error")
	}
}

func TestValidateRejectsInvalidGRPCRuntimeLimits(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.GRPCMaxInFlight = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() err = nil, want max in-flight error")
	}
}

func TestGRPCServerConfigMapsRuntimeLimits(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	runtime := cfg.GRPCServerConfig()
	if runtime.MaxInFlight != cfg.GRPCMaxInFlight {
		t.Fatalf("MaxInFlight = %d, want %d", runtime.MaxInFlight, cfg.GRPCMaxInFlight)
	}
	if runtime.MaxConcurrentStreams != cfg.GRPCMaxConcurrentStreams {
		t.Fatalf("MaxConcurrentStreams = %d, want %d", runtime.MaxConcurrentStreams, cfg.GRPCMaxConcurrentStreams)
	}
	if runtime.AuthRequired != cfg.GRPCAuthRequired {
		t.Fatalf("AuthRequired = %v, want %v", runtime.AuthRequired, cfg.GRPCAuthRequired)
	}
}

func validConfig() Config {
	return Config{
		HTTPAddr:                 ":8080",
		GRPCAddr:                 ":9090",
		GRPCAuthRequired:         true,
		GRPCAuthToken:            "test-token",
		GRPCMaxInFlight:          128,
		GRPCMaxConcurrentStreams: 128,
		GRPCUnaryTimeout:         30 * time.Second,
		GRPCKeepaliveTime:        2 * time.Minute,
		GRPCKeepaliveTimeout:     20 * time.Second,
		GRPCKeepaliveMinTime:     30 * time.Second,
		GRPCMaxRecvMessageBytes:  4 * 1024 * 1024,
		GRPCMaxSendMessageBytes:  4 * 1024 * 1024,
	}
}
