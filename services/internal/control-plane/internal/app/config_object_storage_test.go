package app

import "testing"

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
		{name: "local plaintext without opt in", endpoint: "http://seaweedfs.kodex-system.svc.cluster.local:8333"},
		{name: "credential in endpoint", endpoint: "https://user:secret@s3.example.test"},
		{name: "path in endpoint", endpoint: "https://s3.example.test/bucket"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := Config{
				ObjectStorageEndpoint: test.endpoint, ObjectStorageRegion: "us-east-1",
				ObjectStorageBucket: "kodex-artifacts", ObjectStorageAllowInsecureLocal: test.local,
			}
			if actual := validObjectStorageBoundary(config); actual != test.valid {
				t.Fatalf("validity = %v, want %v", actual, test.valid)
			}
		})
	}
}
