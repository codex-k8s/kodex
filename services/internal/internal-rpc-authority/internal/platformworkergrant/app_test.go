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
	for _, workloadID := range []string{"control-plane", "session-archive", "interaction-gateway", "email-bridge"} {
		if _, ok := supportedWorkloads[workloadID]; !ok {
			t.Fatalf("%s отсутствует в закрытом реестре platform worker", workloadID)
		}
	}
}

func TestOptionalWorkerGrantExactIdentityAndRotation(t *testing.T) {
	for _, workload := range []string{"email-bridge", "interaction-gateway"} {
		t.Run(workload, func(t *testing.T) {
			key, err := internalrpcauth.GenerateES256Key(workload + "-platform-worker-g3")
			if err != nil {
				t.Fatal(err)
			}
			configuration := config{WorkloadID: workload, OutputFile: filepath.Join(t.TempDir(), "grant.jws")}
			now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
			var previous claims
			for i := 0; i < 2; i++ {
				stamp := now.Add(time.Duration(i) * time.Minute)
				if err := rotate(configuration, key, func() time.Time { return stamp }); err != nil {
					t.Fatal(err)
				}
				value := readTestClaims(t, configuration.OutputFile, key)
				if value.WorkloadID != workload || value.Issuer != "https://control-plane.kodex-system.svc.cluster.local/authority/platform-worker/"+workload ||
					value.Audience != "urn:kodex:platform-worker:"+workload || value.CallerSPIFFEID != "spiffe://kodex.local/ns/kodex-system/sa/"+workload ||
					value.CredentialGeneration != 3 || value.Subject != "kodex-system-subject" || value.OrganizationID != "kodex-installation" || value.ProjectID != "" || value.TenantOwner ||
					value.ExpiresAt-value.IssuedAt != int64(grantTTL/time.Second) || value.IssuedAt != stamp.Unix() || value.NotBefore != stamp.Unix() {
					t.Fatal("worker grant binding differs")
				}
				if i > 0 && (value.Revision <= previous.Revision || value.JTI == previous.JTI) {
					t.Fatal("rotation did not advance grant")
				}
				previous = value
			}
		})
	}
}

func TestWorkerGrantRejectsUnknownWorkloadAndForeignKey(t *testing.T) {
	for _, tc := range []struct{ workload, kid string }{
		{"unknown-worker", "unknown-worker-platform-worker-g1"},
		{"email-bridge", "interaction-gateway-platform-worker-g1"},
		{"interaction-gateway", "email-bridge-platform-worker-g1"},
		{"email-bridge", "email-bridge-platform-worker-g1-extra-g2"},
	} {
		t.Run(tc.workload+"-"+tc.kid, func(t *testing.T) {
			key, err := internalrpcauth.GenerateES256Key(tc.kid)
			if err != nil {
				t.Fatal(err)
			}
			configuration := config{WorkloadID: tc.workload, OutputFile: filepath.Join(t.TempDir(), "grant.jws")}
			if err := rotate(configuration, key, time.Now); err == nil {
				t.Fatal("unregistered or foreign signing key accepted")
			}
			if _, err := os.Stat(configuration.OutputFile); !os.IsNotExist(err) {
				t.Fatal("rejected grant was written")
			}
		})
	}
}

func TestGrantReadbackRejectsSignedIdentityMismatch(t *testing.T) {
	key, err := internalrpcauth.GenerateES256Key("email-bridge-platform-worker-g1")
	if err != nil {
		t.Fatal(err)
	}
	configuration := config{WorkloadID: "email-bridge", OutputFile: filepath.Join(t.TempDir(), "grant.jws")}
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	if err := rotate(configuration, key, func() time.Time { return now }); err != nil {
		t.Fatal(err)
	}
	exact := readTestClaims(t, configuration.OutputFile, key)
	for _, tc := range []struct {
		name   string
		mutate func(*claims)
	}{
		{"issuer", func(c *claims) { c.Issuer = "foreign" }},
		{"audience", func(c *claims) { c.Audience = "foreign" }},
		{"spiffe", func(c *claims) { c.CallerSPIFFEID = "foreign" }},
		{"actor", func(c *claims) { c.Subject = "foreign" }},
		{"tenant", func(c *claims) { c.OrganizationID = "foreign" }},
		{"project", func(c *claims) { c.ProjectID = "foreign" }},
		{"owner", func(c *claims) { c.TenantOwner = true }},
		{"version", func(c *claims) { c.Version = 2 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := exact
			tc.mutate(&value)
			raw, err := internalrpcauth.SignCanonicalJSON(value, key, internalrpcauth.ProtectedHeaderExpectation{Type: grantType, KeyID: key.KeyID})
			if err != nil {
				t.Fatal(err)
			}
			if err := writeAtomic(configuration.OutputFile, []byte(raw)); err != nil {
				t.Fatal(err)
			}
			if err := readBack(configuration, key, now); err == nil {
				t.Fatal("signed grant identity mismatch accepted")
			}
		})
	}
}
