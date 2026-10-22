package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/snapshot"
)

// TestTrustKeyConstructorsPreserveTypedJSON compares all four nominal metadata types with literal ordered JSON field layouts, including their distinct generation tags, using one temporary synthetic ES256 key. Public JWK and thumbprint are independently obtained from the library; the test neither grants authority nor runs a production ceremony.
// TestTrustKeyConstructorsPreserveTypedJSON сравнивает четыре отдельных типа метаданных с буквальными упорядоченными JSON-полями, включая разные теги поколения, на одном временном синтетическом ключе ES256. Публичный JWK и отпечаток независимо получаются из библиотеки; тест не выдаёт полномочия и не запускает промышленную процедуру подготовки.
func TestTrustKeyConstructorsPreserveTypedJSON(t *testing.T) {
	t.Parallel()
	key, err := internalrpcauth.GenerateES256Key("constructor-fixture")
	if err != nil {
		t.Fatal("generate constructor fixture key")
	}
	public, err := internalrpcauth.MarshalPublicJWK(key.PublicOnly())
	if err != nil {
		t.Fatal("marshal constructor fixture public key")
	}
	thumbprint, err := internalrpcauth.PublicJWKThumbprintSHA256(key.PublicOnly())
	if err != nil {
		t.Fatal("derive constructor fixture thumbprint")
	}
	for _, test := range []struct {
		name, generationKey string
		value               any
	}{
		{"trustKey", "generation", newTrustKey("NEXT", 2, key, 11, 22)},
		{"readbackManifestKey", "manifest_signer_generation", newReadbackManifestKey("NEXT", 2, key, 11, 22)},
		{"readbackCredentialKey", "credential_signer_generation", newReadbackCredentialKey("NEXT", 2, key, 11, 22)},
		{"restoreRoleTrustKey", "credential_signer_generation", newRestoreRoleTrustKey("NEXT", 2, key, 11, 22)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if reflect.TypeOf(test.value).Name() != test.name {
				t.Fatal("nominal trust-key type changed")
			}
			got, err := json.Marshal(test.value)
			want := fmt.Sprintf(`{"status":"NEXT","%s":2,"kid":%q,"public_jwk":%s,"jwk_thumbprint_sha256":%q,"not_before":11,"not_after":22}`, test.generationKey, key.KeyID, public, thumbprint)
			if err != nil || string(got) != want {
				t.Fatal("trust-key JSON layout, values or generation tag changed")
			}
		})
	}
}

// TestGenerateCreatesUsableRestoreRoleTrust generates synthetic signers and offline material in a temporary directory at a fixed time, makes the required public/trust files read-only and loads the restore-role trust through the real snapshot parser. It checks first revisions, CURRENT/NEXT restore keys and the next private-key output's existence; it does not install material, rotate live roots or fully exercise the readback chain.
// TestGenerateCreatesUsableRestoreRoleTrust создаёт синтетических подписантов и автономные материалы во временном каталоге на фиксированное время, делает нужные публичные файлы и файлы доверия read-only и загружает доверие роли восстановления реальным разборщиком snapshot. Проверяются первые ревизии, ключи restore CURRENT/NEXT и наличие вывода следующего приватного ключа; материалы не устанавливаются, живые корни не ротируются, полная цепочка readback не проверяется.
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
