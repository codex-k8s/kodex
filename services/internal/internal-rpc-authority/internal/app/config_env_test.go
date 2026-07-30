package app

import (
	"testing"
	"time"
)

func TestParseEnvironmentPreservesDefaultsAndRejectsInvalidTypes(t *testing.T) {
	type sampleConfig struct {
		Revision uint64        `env:"AUTHORITY_TEST_REVISION"`
		Timeout  time.Duration `env:"AUTHORITY_TEST_TIMEOUT"`
		Endpoint string        `env:"AUTHORITY_TEST_ENDPOINT"`
	}
	t.Setenv("AUTHORITY_TEST_REVISION", "7")
	t.Setenv("AUTHORITY_TEST_TIMEOUT", "3s")
	config := sampleConfig{Endpoint: "default.svc:8443"}
	if err := parseEnvironment(&config); err != nil {
		t.Fatalf("parse valid environment: %v", err)
	}
	if config.Revision != 7 ||
		config.Timeout != 3*time.Second ||
		config.Endpoint != "default.svc:8443" {
		t.Fatalf("unexpected parsed config: %#v", config)
	}

	t.Setenv("AUTHORITY_TEST_REVISION", "not-a-number")
	if err := parseEnvironment(&config); err == nil {
		t.Fatal("invalid typed environment value accepted")
	}
}

func TestLoadReadbackConfigUsesTypedEnvironment(t *testing.T) {
	t.Setenv(
		"INTERNAL_RPC_AUTHORITY_POSTGRES_EXPECTED_SESSION_USER",
		"internal_rpc_authority_readback_attestor_g1",
	)
	t.Setenv("INTERNAL_RPC_AUTHORITY_READBACK_VERIFIER_GENERATION", "5")
	t.Setenv("INTERNAL_RPC_AUTHORITY_SHUTDOWN_TIMEOUT", "12s")
	config, err := LoadReadbackConfig()
	if err != nil {
		t.Fatalf("load readback config: %v", err)
	}
	if config.VerifierGeneration != 5 ||
		config.ShutdownTimeout != 12*time.Second {
		t.Fatalf("typed environment was not applied: %#v", config)
	}

	t.Setenv("INTERNAL_RPC_AUTHORITY_READBACK_VERIFIER_GENERATION", "invalid")
	if _, err := LoadReadbackConfig(); err == nil {
		t.Fatal("invalid readback generation accepted")
	}
}

func TestLoadRestoreOperatorConfigUsesTypedTime(t *testing.T) {
	t.Setenv("INTERNAL_RPC_AUTHORITY_RESTORE_ACTION", "prepare")
	t.Setenv("INTERNAL_RPC_AUTHORITY_RESTORE_ID", "restore-20260730")
	t.Setenv(
		"INTERNAL_RPC_AUTHORITY_BACKUP_MANIFEST_DIGEST_SHA256",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	)
	t.Setenv("INTERNAL_RPC_AUTHORITY_RECOVERY_TARGET", "2026-07-30T17:00:00Z")
	t.Setenv("INTERNAL_RPC_AUTHORITY_IDEMPOTENCY_KEY", "restore-20260730")
	t.Setenv("INTERNAL_RPC_AUTHORITY_CORRELATION_ID", "restore-20260730")
	config, err := LoadRestoreOperatorConfig()
	if err != nil {
		t.Fatalf("load restore operator config: %v", err)
	}
	if !config.RecoveryTarget.Equal(
		time.Date(2026, 7, 30, 17, 0, 0, 0, time.UTC),
	) {
		t.Fatalf("unexpected recovery target: %s", config.RecoveryTarget)
	}
}
