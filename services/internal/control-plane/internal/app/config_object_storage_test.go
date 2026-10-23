package app

import "testing"

// TestValidObjectStorageBoundary permits HTTPS and the exact opted-in local origin while rejecting endpoint escape fields.
// RU: TestValidObjectStorageBoundary допускает HTTPS и точный явно включённый локальный адрес, отклоняя обходы через поля URL.
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
				ObjectStorageBucket: "kodex-artifacts", SessionArchiveStorageBucket: "kodex-session-archives", ObjectStorageAllowInsecureLocal: test.local,
			}
			if actual := validObjectStorageBoundary(config); actual != test.valid {
				t.Fatalf("validity = %v, want %v", actual, test.valid)
			}
		})
	}
}

// TestObjectStorageBoundaryRetainsRegionAndBucketChecks verifies that the shared endpoint predicate does not replace service-specific validation.
// RU: TestObjectStorageBoundaryRetainsRegionAndBucketChecks проверяет, что общий предикат адреса не заменяет сервисные проверки региона и бакетов.
func TestObjectStorageBoundaryRetainsRegionAndBucketChecks(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		region  string
		bucket  string
		archive string
	}{
		{name: "missing region", bucket: "artifacts", archive: "archives"},
		{name: "invalid region", region: "us east 1", bucket: "artifacts", archive: "archives"},
		{name: "missing artifact bucket", region: "us-east-1", archive: "archives"},
		{name: "invalid artifact bucket", region: "us-east-1", bucket: "Artifacts", archive: "archives"},
		{name: "missing archive bucket", region: "us-east-1", bucket: "artifacts"},
		{name: "invalid archive bucket", region: "us-east-1", bucket: "artifacts", archive: "archives/other"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := Config{ObjectStorageEndpoint: "https://s3.example.test", ObjectStorageRegion: test.region,
				ObjectStorageBucket: test.bucket, SessionArchiveStorageBucket: test.archive}
			if validObjectStorageBoundary(config) {
				t.Fatal("valid endpoint bypassed region or bucket validation")
			}
		})
	}
}
