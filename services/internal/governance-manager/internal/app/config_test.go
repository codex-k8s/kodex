package app

import (
	"strings"
	"testing"
	"time"
)

func TestConfigValidateRequiresGRPCTokenWhenAuthEnabled(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.GRPCAuthToken = ""
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "KODEX_GOVERNANCE_MANAGER_GRPC_AUTH_TOKEN") {
		t.Fatalf("Validate() error = %v, want missing grpc token", err)
	}
}

func TestConfigValidateAllowsDisabledGRPCAuth(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.GRPCAuthRequired = false
	cfg.GRPCAuthToken = ""
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestConfigValidateRejectsInvalidGRPCLimit(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.GRPCMaxInFlight = 0
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "grpc max in-flight") {
		t.Fatalf("Validate() error = %v, want grpc max in-flight error", err)
	}
}

func validConfig() Config {
	return Config{
		HTTPAddr:                 ":8080",
		GRPCAddr:                 ":9090",
		GRPCAuthRequired:         true,
		GRPCAuthToken:            "test-token",
		GRPCMaxConcurrentStreams: 128,
		GRPCMaxInFlight:          128,
		GRPCMaxRecvMessageBytes:  4 << 20,
		GRPCMaxSendMessageBytes:  4 << 20,
		GRPCKeepaliveMinTime:     30 * time.Second,
		GRPCKeepaliveTime:        2 * time.Minute,
		GRPCKeepaliveTimeout:     20 * time.Second,
		GRPCUnaryTimeout:         30 * time.Second,
	}
}
