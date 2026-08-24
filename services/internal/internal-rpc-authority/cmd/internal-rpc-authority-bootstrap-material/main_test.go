package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/snapshot"
)

func TestGenerateCreatesUsableRestoreRoleTrust(t *testing.T) {
	t.Parallel()

	manifestSigner, err := internalrpcauth.GenerateES256Key("ira-manifest-signer-g1")
	if err != nil {
		t.Fatalf("generate manifest signer: %v", err)
	}
	readbackSigner, err := internalrpcauth.GenerateES256Key("ira-readback-credential-g1")
	if err != nil {
		t.Fatalf("generate readback signer: %v", err)
	}
	restoreSigner, err := internalrpcauth.GenerateES256Key("ira-publisher-restore-g1")
	if err != nil {
		t.Fatalf("generate restore signer: %v", err)
	}
	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	output := t.TempDir()
	if err := generate(output, manifestSigner, readbackSigner, restoreSigner, now); err != nil {
		t.Fatalf("generate authority bootstrap material: %v", err)
	}
	for _, relativePath := range []string{
		"public/manifest-root/bootstrap-public.jwk",
		"public/manifest-root/bootstrap-metadata.json",
		"external/publisher-manifest-trust.jws",
		"external/restore-role-trust.jws",
	} {
		if err := os.Chmod(filepath.Join(output, relativePath), 0o400); err != nil {
			t.Fatalf("make generated trust material read-only: %v", err)
		}
	}

	keys, metadata, err := snapshot.LoadRestoreRoleTrust(snapshot.RestoreRoleTrustOptions{
		ManifestRootPublicJWKFile: filepath.Join(
			output,
			"public/manifest-root/bootstrap-public.jwk",
		),
		ManifestRootMetadataFile: filepath.Join(
			output,
			"public/manifest-root/bootstrap-metadata.json",
		),
		ManifestTrustBundleJWSFile: filepath.Join(
			output,
			"external/publisher-manifest-trust.jws",
		),
		RestoreRoleTrustJWSFile: filepath.Join(
			output,
			"external/restore-role-trust.jws",
		),
		Now: now,
	})
	if err != nil {
		t.Fatalf("load generated restore role trust: %v", err)
	}
	if metadata.SourceRevision != 1 || metadata.KeySetRevision != 1 ||
		metadata.SignerGeneration != 1 {
		t.Fatalf("unexpected restore trust metadata: %+v", metadata)
	}
	if keys["ira-publisher-restore-g1"].Status != "CURRENT" ||
		keys["ira-publisher-restore-g2"].Status != "NEXT" {
		t.Fatalf("unexpected restore role trust keys: %+v", keys)
	}
	if _, err := os.Stat(filepath.Join(
		output,
		"offline/restore-signer-next-private.jwk",
	)); err != nil {
		t.Fatalf("read generated next restore signer: %v", err)
	}
}
