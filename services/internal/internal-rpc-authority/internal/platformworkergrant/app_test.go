package platformworkergrant

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
)

func TestRotateWritesExactBoundedGrant(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	key, err := internalrpcauth.GenerateES256Key("runtime-controller-platform-worker-g1")
	if err != nil {
		t.Fatal(err)
	}
	configuration := config{
		WorkloadID: "runtime-controller",
		OutputFile: filepath.Join(directory, "application-grant.jws"),
	}
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	if err := rotate(configuration, key, func() time.Time { return now }); err != nil {
		t.Fatalf("материализовать grant: %v", err)
	}
	if err := readBack(configuration, key, now); err != nil {
		t.Fatalf("проверить grant: %v", err)
	}
	info, err := os.Stat(configuration.OutputFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o440 {
		t.Fatalf("небезопасные права grant: %o", info.Mode().Perm())
	}
}

func TestRotateAdvancesGrantRevisionWithoutChangingCredentialGeneration(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	key, err := internalrpcauth.GenerateES256Key("image-promotion-platform-worker-g7")
	if err != nil {
		t.Fatal(err)
	}
	configuration := config{WorkloadID: "image-promotion", OutputFile: filepath.Join(directory, "application-grant.jws")}
	first := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	if err := rotate(configuration, key, func() time.Time { return first }); err != nil {
		t.Fatal(err)
	}
	firstClaims := readTestClaims(t, configuration.OutputFile, key)
	second := first.Add(time.Minute)
	if err := rotate(configuration, key, func() time.Time { return second }); err != nil {
		t.Fatal(err)
	}
	secondClaims := readTestClaims(t, configuration.OutputFile, key)
	if firstClaims.Revision >= secondClaims.Revision || firstClaims.JTI == secondClaims.JTI {
		t.Fatal("штатное обновление не выпустило новый grant")
	}
	if firstClaims.CredentialGeneration != 7 || secondClaims.CredentialGeneration != 7 {
		t.Fatalf("поколение credential изменилось при обновлении: %d -> %d", firstClaims.CredentialGeneration, secondClaims.CredentialGeneration)
	}
}

func readTestClaims(t *testing.T, path string, key internalrpcauth.ES256Key) claims {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := internalrpcauth.VerifyCanonicalJSON(string(raw), key.PublicOnly(), internalrpcauth.ProtectedHeaderExpectation{Type: grantType, KeyID: key.KeyID})
	if err != nil {
		t.Fatal(err)
	}
	var value claims
	if err := internalrpcauth.DecodeCanonicalJSON(verified.CanonicalPayload, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestWriteAtomicRejectsSymlinkDirectory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(filepath.Join(link, "grant.jws"), []byte("signed")); err == nil {
		t.Fatal("symlink output directory был принят")
	}
}

func TestSupportedWorkloadsIncludeAuthorityCallers(t *testing.T) {
	t.Parallel()
	for _, workloadID := range []string{"control-plane", "session-archive"} {
		if _, ok := supportedWorkloads[workloadID]; !ok {
			t.Fatalf("%s отсутствует в закрытом реестре platform worker", workloadID)
		}
	}
}
