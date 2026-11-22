package app

import (
	"testing"
	"time"
)

// validConfig supplies a bounded production-shaped baseline for negative validation tests.
// RU: validConfig задаёт ограниченную production-подобную основу для отрицательных тестов валидации.
func validConfig() Config {
	return Config{
		Environment: "production", TechnicalListen: ":9090", InstanceID: "retention-0",
		PostgresDSNFile: "/secrets/dsn", PostgresCAFile: "/config/ca.pem", PostgresTLSServerName: "postgres.example",
		PostgresMaxConnections: 2, ObjectStorageEndpoint: "https://s3.example", ObjectStorageRegion: "us-east-1", ObjectStorageBucket: "artifacts",
		ObjectStorageAccessKeyFile: "/secrets/access", ObjectStorageSecretKeyFile: "/secrets/secret",
		StartupTimeout: 30 * time.Second, ShutdownTimeout: 20 * time.Second, OperationTimeout: 20 * time.Second,
		PollInterval: 30 * time.Second, ReadinessInterval: 10 * time.Second, ClaimLease: 2 * time.Minute, BatchSize: 32,
		ControlPlaneTarget: "control-plane.example:8443", ControlPlaneTLSServerName: "control-plane.example",
		ControlPlaneCAFile: "/config/owner-ca.pem", ControlPlaneCertificateFile: "/secrets/tls.crt",
		ControlPlanePrivateKeyFile: "/secrets/tls.key", ApplicationGrantFile: "/secrets/grant.jws",
		CaptureTimeout: 90 * time.Second, CapturePollInterval: 2 * time.Second, CaptureLease: 3 * time.Minute,
	}
}

// TestConfigRejectsInsecureObjectStorageWithoutExplicitLocalOverride denies implicit HTTP transport.
// RU: TestConfigRejectsInsecureObjectStorageWithoutExplicitLocalOverride запрещает неявный HTTP transport.
func TestConfigRejectsInsecureObjectStorageWithoutExplicitLocalOverride(t *testing.T) {
	config := validConfig()
	config.ObjectStorageEndpoint = "http://s3.example"
	if err := config.validate(); err == nil {
		t.Fatal("validate() accepted insecure object storage without override")
	}
}

// TestConfigRequiresLeaseLongerThanOperation ensures cleanup cannot outlive its claim lease.
// RU: TestConfigRequiresLeaseLongerThanOperation проверяет, что очистка не переживает срок своего claim.
func TestConfigRequiresLeaseLongerThanOperation(t *testing.T) {
	config := validConfig()
	config.ClaimLease = config.OperationTimeout
	if err := config.validate(); err == nil {
		t.Fatal("validate() accepted an unfenced operation window")
	}
}
