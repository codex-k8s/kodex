package readinessgrant

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
)

type recordedPatch struct {
	name  string
	key   string
	value []byte
}

type recordingPatcher struct{ patches []recordedPatch }

func (patcher *recordingPatcher) PatchSecret(_ context.Context, _ string, name, key string, value []byte) error {
	patcher.patches = append(patcher.patches, recordedPatch{name: name, key: key, value: append([]byte(nil), value...)})
	return nil
}

func TestDefaultTargetsUseDedicatedSecrets(t *testing.T) {
	t.Parallel()
	targets := DefaultTargets("/var/run/readiness-signers")
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		if target.SecretName == "interaction-gateway-runtime" {
			t.Fatal("readiness rotator must not patch the shared interaction runtime Secret")
		}
		if _, duplicate := seen[target.SecretName]; duplicate {
			t.Fatalf("readiness Secret is reused: %s", target.SecretName)
		}
		seen[target.SecretName] = struct{}{}
	}
	if len(targets) != 5 {
		t.Fatalf("unexpected readiness target count: %d", len(targets))
	}
}

func TestRotateIssuesExactShortLivedGrant(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	target := readinessTarget(directory, "runtime-controller", "control-plane.runtime-readiness",
		"runtime-controller-application-grant", "application-grant.jws")
	key, err := internalrpcauth.GenerateES256Key("readiness-test-g1")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := internalrpcauth.MarshalPrivateJWK(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "runtime-controller.private.jwk"), raw, 0o640); err != nil {
		t.Fatal(err)
	}
	patcher := &recordingPatcher{}
	rotator, err := New("mattercodex-system", 4*time.Minute, time.Minute, []Target{target}, patcher)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 12, 4, 0, 0, 0, time.UTC)
	rotator.now = func() time.Time { return now }
	if err := rotator.Rotate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !rotator.Ready() || len(patcher.patches) != 1 {
		t.Fatalf("unexpected rotation state: ready=%v patches=%d", rotator.Ready(), len(patcher.patches))
	}
	patch := patcher.patches[0]
	if patch.name != target.SecretName || patch.key != target.SecretDataKey {
		t.Fatalf("unexpected Secret target: %s/%s", patch.name, patch.key)
	}
	verified, err := internalrpcauth.VerifyCanonicalJSON(string(patch.value), key.PublicOnly(),
		internalrpcauth.ProtectedHeaderExpectation{Type: grantType, KeyID: key.KeyID})
	if err != nil {
		t.Fatal(err)
	}
	var actual claims
	if err := internalrpcauth.DecodeCanonicalJSON(verified.CanonicalPayload, &actual); err != nil {
		t.Fatal(err)
	}
	if actual.WorkloadID != target.WorkloadID || actual.CallerSPIFFEID != target.CallerSPIFFE ||
		actual.ProjectID != "" || actual.ExpiresAt-actual.IssuedAt != 240 || actual.Revision != uint64(now.Unix()) {
		t.Fatalf("unexpected readiness claims: %+v", actual)
	}
}
