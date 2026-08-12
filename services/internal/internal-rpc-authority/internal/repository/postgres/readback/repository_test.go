package readback

import (
	"testing"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
)

func TestReadbackTrustReadinessArgsMatchQuery(t *testing.T) {
	args := readbackTrustReadinessArgs(model.ReadbackTrustState{})
	expected := []string{
		"root_id",
		"root_fingerprint_sha256",
		"manifest_bundle_revision",
		"manifest_bundle_digest_sha256",
		"trust_source_revision",
		"trust_set_digest_sha256",
		"trust_key_set_revision",
		"signer_generation",
		"served_state_digest_sha256",
	}
	if len(args) != len(expected) {
		t.Fatalf("unexpected readback readiness argument count: got %d, want %d", len(args), len(expected))
	}
	for _, name := range expected {
		if _, ok := args[name]; !ok {
			t.Fatalf("readback readiness argument %q is missing", name)
		}
	}
}
