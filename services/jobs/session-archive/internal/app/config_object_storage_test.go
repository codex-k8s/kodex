package app

import (
	"testing"
	"time"
)

func TestValidObjectStorageBoundary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		endpoint string
		local    bool
		valid    bool
	}{
		{name: "production https", endpoint: "https://s3.example.test", valid: true},
		{name: "local SeaweedFS", endpoint: "http://seaweedfs-s3.kodex-system.svc.cluster.local:8333", local: true, valid: true},
		{name: "other local service", endpoint: "http://other.kodex-system.svc.cluster.local:8333", local: true},
		{name: "local SeaweedFS wrong port", endpoint: "http://seaweedfs-s3.kodex-system.svc.cluster.local:9000", local: true},
		{name: "external plaintext", endpoint: "http://s3.example.test", local: true},
		{name: "local plaintext without opt in", endpoint: "http://seaweedfs-s3.kodex-system.svc.cluster.local:8333"},
		{name: "credential in endpoint", endpoint: "https://user:secret@s3.example.test"},
		{name: "path in endpoint", endpoint: "https://s3.example.test/bucket"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := Config{ObjectStorageEndpoint: test.endpoint, ObjectStorageAllowInsecureLocal: test.local}
			if actual := validObjectStorageBoundary(config); actual != test.valid {
				t.Fatalf("validity = %v, want %v", actual, test.valid)
			}
		})
	}
}

func TestConfigRejectsInsecureLocalOptInInProduction(t *testing.T) {
	t.Parallel()
	config := Config{
		Environment: "production", Namespace: "kodex-system", InstanceID: "instance-1",
		TechnicalListen: ":9090", ControlPlaneTarget: "control-plane:8443",
		ControlPlaneTLSServerName: "control-plane.kodex-system.svc.cluster.local",
		ControlPlaneCAFile:        "/tls/ca.pem", ControlPlaneCertificateFile: "/tls/tls.crt",
		ControlPlanePrivateKeyFile: "/tls/tls.key", ApplicationGrantFile: "/grant/application.jws",
		WorkerImage: "registry.example.test/session-archive@sha256:digest", WorkerServiceAccount: "session-archive-worker",
		SessionPVCSize: "20Gi", ObjectStorageSecret: "kodex-external-s3",
		ObjectStorageEndpoint: "https://s3.example.test", ObjectStorageRegion: "us-east-1",
		ObjectStorageBucket: "kodex-session-archives", ObjectStorageAllowInsecureLocal: true,
		RPCDeadline: 5 * time.Second, PollInterval: time.Second,
		ReadinessInterval: 10 * time.Second, WorkerTimeout: 8 * time.Minute,
	}
	if err := config.validate(); err == nil {
		t.Fatal("production configuration accepted insecure local opt-in")
	}
}
