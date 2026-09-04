package platform

import (
	"strings"
	"testing"
)

func TestWorkerGrantHighWatermarkUsesCredentialGenerationAndRevision(t *testing.T) {
	for _, required := range []string{
		"(workload_id, credential_generation, revision, issued_at, expires_at)",
		"credential_generation < EXCLUDED.credential_generation",
		"credential_generation = EXCLUDED.credential_generation",
		"revision < EXCLUDED.revision",
		"AND credential_generation = $2",
		"AND revision = $3",
	} {
		if !strings.Contains(queryAcceptWorkerGrantHighWatermark, required) {
			t.Fatalf("worker grant watermark lost invariant %q", required)
		}
	}
}
