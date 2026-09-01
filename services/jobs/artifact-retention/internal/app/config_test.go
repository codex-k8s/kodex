package app

import (
	"testing"
	"time"
)

func validConfig() Config {
	return Config{
		Environment: "production", TechnicalListen: ":9090", InstanceID: "retention-0",
		PostgresDSNFile: "/secrets/dsn", PostgresCAFile: "/config/ca.pem", PostgresTLSServerName: "postgres.example",
		PostgresMaxConnections: 2, ObjectStorageEndpoint: "https://s3.example", ObjectStorageRegion: "us-east-1", ObjectStorageBucket: "artifacts",
		ObjectStorageAccessKeyFile: "/secrets/access", ObjectStorageSecretKeyFile: "/secrets/secret",
		StartupTimeout: 30 * time.Second, ShutdownTimeout: 20 * time.Second, OperationTimeout: 20 * time.Second,
		PollInterval: 30 * time.Second, ReadinessInterval: 10 * time.Second, ClaimLease: 2 * time.Minute, BatchSize: 32,
	}
}

func TestConfigRejectsInsecureObjectStorageWithoutExplicitLocalOverride(t *testing.T) {
	config := validConfig()
	config.ObjectStorageEndpoint = "http://s3.example"
	if err := config.validate(); err == nil {
		t.Fatal("validate() accepted insecure object storage without override")
	}
}

func TestConfigRequiresLeaseLongerThanOperation(t *testing.T) {
	config := validConfig()
	config.ClaimLease = config.OperationTimeout
	if err := config.validate(); err == nil {
		t.Fatal("validate() accepted an unfenced operation window")
	}
}
