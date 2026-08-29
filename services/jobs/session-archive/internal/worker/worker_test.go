package worker

import (
	"testing"
	"time"
)

func TestValidateConfigAllowsOnlyExplicitExactLocalObjectStorage(t *testing.T) {
	t.Parallel()

	base := config{
		Environment: "staging", TaskFile: "/task.json", Workspace: "/workspace", ResultFile: "/result.json",
		Endpoint: "http://seaweedfs-s3.kodex-system.svc.cluster.local:8333", Region: "us-east-1", Bucket: "archives",
		AccessKeyFile: "/access-key", SecretKeyFile: "/secret-key", UsePathStyle: true, Timeout: time.Minute,
	}
	if err := validateConfig(base); err == nil {
		t.Fatal("worker accepted plaintext object storage without the local exception")
	}
	base.AllowInsecureLocal = true
	if err := validateConfig(base); err != nil {
		t.Fatalf("worker rejected the exact local object storage exception: %v", err)
	}
	base.Endpoint = "http://other.kodex-system.svc.cluster.local:8333"
	if err := validateConfig(base); err == nil {
		t.Fatal("worker accepted the local plaintext exception for another host")
	}
	base.Endpoint = "https://s3.example.test"
	base.AllowInsecureLocal = false
	if err := validateConfig(base); err != nil {
		t.Fatalf("worker rejected TLS object storage: %v", err)
	}
}

func TestValidateConfigRejectsLocalExceptionInProduction(t *testing.T) {
	t.Parallel()

	value := config{
		Environment: "production", TaskFile: "/task.json", Workspace: "/workspace", ResultFile: "/result.json",
		Endpoint: "http://seaweedfs-s3.kodex-system.svc.cluster.local:8333", Region: "us-east-1", Bucket: "archives",
		AccessKeyFile: "/access-key", SecretKeyFile: "/secret-key", AllowInsecureLocal: true, Timeout: time.Minute,
	}
	if err := validateConfig(value); err == nil {
		t.Fatal("worker accepted the local plaintext exception in production")
	}
}
