package snapshot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/service"
)

// TestBootstrapRootMetadataIsCanonical parses both committed bootstrap root metadata fixtures through
// the canonical decoder; it does not verify a live root's provenance or mount policy.
// TestBootstrapRootMetadataIsCanonical разбирает оба корневых файла оснастки из репозитория каноническим
// декодером; происхождение живого корня и правила монтирования не проверяются.
func TestBootstrapRootMetadataIsCanonical(t *testing.T) {
	for _, name := range []string{"manifest-root", "readback-root"} {
		path := filepath.Join("..", "bootstrap", name, "bootstrap-metadata.json")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s metadata: %v", name, err)
		}
		var value map[string]json.RawMessage
		if err := internalrpcauth.DecodeCanonicalJSON(raw, &value); err != nil {
			t.Fatalf("%s metadata is not canonical: %v", name, err)
		}
	}
}

// TestReadRegularFileAcceptsExactGroupReadableSecret checks acceptance of a synthetic regular file with
// exact 0440 mode under the shared reader's size budget.
// TestReadRegularFileAcceptsExactGroupReadableSecret проверяет принятие синтетического обычного файла с
// точным режимом 0440 в бюджете размера общего чтения.
func TestReadRegularFileAcceptsExactGroupReadableSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private.jwk")
	if err := os.WriteFile(path, []byte(`{"kty":"EC"}`), 0o440); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if _, err := readRegularFile(path, 1024); err != nil {
		t.Fatalf("group-readable projected secret rejected: %v", err)
	}
}

// TestReadRegularFileAcceptsReadOnlyContainerScopedMaterial checks exact 0444 file acceptance; the
// fixture is not proof of a deployed container's isolation.
// TestReadRegularFileAcceptsReadOnlyContainerScopedMaterial проверяет принятие точного режима 0444;
// оснастка не доказывает изоляцию развёрнутого контейнера.
func TestReadRegularFileAcceptsReadOnlyContainerScopedMaterial(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private.jwk")
	if err := os.WriteFile(path, []byte(`{"kty":"EC"}`), 0o444); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if _, err := readRegularFile(path, 1024); err != nil {
		t.Fatalf("read-only projected material rejected: %v", err)
	}
}

// TestReadRegularFileRejectsWritableMaterial requires a synthetic 0600 writable file to fail the
// read-only material contract.
// TestReadRegularFileRejectsWritableMaterial требует отказа для синтетического записываемого файла 0600
// по контракту материалов только для чтения.
func TestReadRegularFileRejectsWritableMaterial(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private.jwk")
	if err := os.WriteFile(path, []byte(`{"kty":"EC"}`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if _, err := readRegularFile(path, 1024); err == nil {
		t.Fatal("writable private material accepted")
	}
}

// TestReadRegularFileRejectsEscapingSymlink checks rejection when a synthetic projected-path symlink
// escapes its parent mount root.
// TestReadRegularFileRejectsEscapingSymlink проверяет отказ для синтетической ссылки проецируемого пути,
// выходящей за родительский корень монтирования.
func TestReadRegularFileRejectsEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "snapshot.jws")
	if err := os.WriteFile(target, []byte("header.payload.signature"), 0o440); err != nil {
		t.Fatalf("write target: %v", err)
	}
	path := filepath.Join(root, "snapshot.jws")
	if err := os.Symlink(target, path); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	if _, err := readRegularFile(path, 1024); err == nil {
		t.Fatal("escaping projected-file symlink accepted")
	}
}

// TestValidateHistoryAllowsBoundedCatchUp permits a contiguous suffix shorter than full history when its
// last revision and digest match the required predecessor.
// TestValidateHistoryAllowsBoundedCatchUp разрешает непрерывный суффикс короче полной истории, если
// последняя ревизия и хеш совпадают с обязательным предшественником.
func TestValidateHistoryAllowsBoundedCatchUp(t *testing.T) {
	history := []revisionDigest{
		{Revision: 8, DigestSHA256: repeatedDigest("8")},
		{Revision: 9, DigestSHA256: repeatedDigest("9")},
	}
	if err := validateHistory(
		10,
		revisionDigest{Revision: 9, DigestSHA256: repeatedDigest("9")},
		history,
	); err != nil {
		t.Fatalf("valid catch-up history rejected: %v", err)
	}
}

// TestValidateHistoryRejectsGapMutationAndMissingPredecessor rejects gapped, duplicated or
// tail-digest-mutated predecessor histories without persistence or network access.
// TestValidateHistoryRejectsGapMutationAndMissingPredecessor отклоняет разрывы, повторы и подмену хеша
// хвоста истории без хранилища и сети.
func TestValidateHistoryRejectsGapMutationAndMissingPredecessor(t *testing.T) {
	for name, history := range map[string][]revisionDigest{
		"gap": {
			{Revision: 7, DigestSHA256: repeatedDigest("7")},
			{Revision: 9, DigestSHA256: repeatedDigest("9")},
		},
		"duplicate": {
			{Revision: 8, DigestSHA256: repeatedDigest("8")},
			{Revision: 8, DigestSHA256: repeatedDigest("8")},
		},
		"wrong-predecessor-digest": {
			{Revision: 8, DigestSHA256: repeatedDigest("8")},
			{Revision: 9, DigestSHA256: repeatedDigest("a")},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateHistory(
				10,
				revisionDigest{Revision: 9, DigestSHA256: repeatedDigest("9")},
				history,
			); err == nil {
				t.Fatal("invalid signed history accepted")
			}
		})
	}
}

// TestBuildPolicySnapshotUsesCurrentWorkloadKeyGeneration pins domain signer generation to the current
// workload key even when the outer manifest generation differs.
// TestBuildPolicySnapshotUsesCurrentWorkloadKeyGeneration закрепляет доменное поколение подписанта за
// текущим ключом нагрузки, даже если поколение внешнего манифеста отличается.
func TestBuildPolicySnapshotUsesCurrentWorkloadKeyGeneration(t *testing.T) {
	key, err := internalrpcauth.GenerateES256Key("issuer-g2")
	if err != nil {
		t.Fatalf("generate issuer key: %v", err)
	}
	policy := buildPolicySnapshot(
		document{SignerGeneration: 1},
		service.VerificationKeyRecord{
			Key: key.PublicOnly(), Generation: 2,
			Issuer: "spiffe://kodex.local/ns/kodex-system/sa/issuer",
		},
		repeatedDigest("a"),
		nil,
	)
	if policy.SignerGeneration != 2 {
		t.Fatalf(
			"policy signer generation = %d, want current workload generation 2",
			policy.SignerGeneration,
		)
	}
}

// repeatedDigest repeats a nonempty fixture string and truncates it to 64 bytes; tests supply safe ASCII
// inputs and the helper does not validate digest syntax.
// repeatedDigest повторяет непустую строку оснастки и усекает до 64 байтов; тесты передают безопасный
// ASCII, а синтаксис хеша помощник не проверяет.
func repeatedDigest(value string) string {
	result := ""
	for len(result) < 64 {
		result += value
	}
	return result[:64]
}
