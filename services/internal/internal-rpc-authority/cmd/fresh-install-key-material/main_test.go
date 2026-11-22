package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
)

// TestFreshKeyMaterialProcess runs its own test binary as a child to execute fresh-key generation only in a temporary output directory, then checks all twelve worker identities, file/directory modes, public/private correspondence, absence of a private scalar in public JSON and distinct public coordinates. It does not validate all nonworker entries or install a Secret; the output environment marker is only the subprocess harness.
// TestFreshKeyMaterialProcess запускает собственный тестовый бинарь дочерним процессом для создания ключей только во временном каталоге, затем проверяет двенадцать идентичностей worker, права файлов и каталогов, соответствие публичных и приватных частей, отсутствие приватного скаляра в публичном JSON и разные публичные координаты. Остальные записи проверяются не полностью, Secret не устанавливается; переменная выходного каталога служит только оснастке дочернего процесса.
func TestFreshKeyMaterialProcess(t *testing.T) {
	if os.Getenv("KODEX_TEST_FRESH_KEYS_OUTPUT") != "" {
		os.Args = []string{"fresh-install-key-material", os.Getenv("KODEX_TEST_FRESH_KEYS_OUTPUT")}
		main()
		return
	}
	t.Parallel()
	output := t.TempDir()
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestFreshKeyMaterialProcess$")
	command.Env = append(os.Environ(), "KODEX_TEST_FRESH_KEYS_OUTPUT="+output)
	if err := command.Run(); err != nil {
		t.Fatalf("fresh key generator: %v", err)
	}
	keys := map[string]bool{}
	for _, workload := range []string{
		"automation-scheduler", "session-archive", "artifact-retention", "integration-gateway", "interaction-gateway",
		"email-bridge", "runtime-controller", "role-image-builder", "image-admission",
		"image-promotion", "secret-broker", "control-plane",
	} {
		directory := filepath.Join(output, "platform-worker", workload)
		info, err := os.Stat(directory)
		if err != nil || info.Mode().Perm() != 0o700 {
			t.Fatal("worker key directory permissions mismatch")
		}
		read := func(name string) []byte {
			t.Helper()
			file := filepath.Join(directory, name)
			info, err := os.Stat(file)
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
				t.Fatal("worker key file permissions mismatch")
			}
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal("worker key file cannot be read")
			}
			return data
		}
		private, err := internalrpcauth.ParsePrivateJWK(read("private.jwk"))
		if err != nil || private.KeyID != workload+"-platform-worker-g1" {
			t.Fatal("worker private key identity mismatch")
		}
		public := read("public.jwk")
		parsed, err := internalrpcauth.ParsePublicJWK(public)
		if err != nil || parsed.KeyID != private.KeyID {
			t.Fatal("worker public key identity mismatch")
		}
		derived, err := internalrpcauth.MarshalPublicJWK(private)
		if err != nil || !bytes.Equal(derived, public) {
			t.Fatal("worker public key does not match private key")
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(public, &fields); err != nil {
			t.Fatal("public key JSON is invalid")
		}
		if _, present := fields["d"]; present {
			t.Fatal("public trust includes private material")
		}
		identity := string(fields["x"]) + ":" + string(fields["y"])
		if keys[identity] {
			t.Fatal("different workloads share a signing key")
		}
		keys[identity] = true
	}
	entries, err := os.ReadDir(filepath.Join(output, "platform-worker"))
	if err != nil || len(entries) != len(keys) {
		t.Fatal("fresh worker key registry differs from the closed workload set")
	}
}

// TestArtifactRetentionWorkerKeyMaterial checks the additive scope, protecting existing key bytes and the exact registered key ID.
// TestArtifactRetentionWorkerKeyMaterial проверяет адресный режим, сохранность существующего ключа и точный зарегистрированный ID.
func TestArtifactRetentionWorkerKeyMaterial(t *testing.T) {
	t.Parallel()
	parent := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal("prepare private issuer fixture directory", err)
	}
	directory := filepath.Join(parent, "issuer")
	if err := generateArtifactRetentionIssuerMaterial(directory); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 5 {
		t.Fatal("additive scope generated unexpected material")
	}
	privatePath := filepath.Join(directory, "private.jwk")
	private, err := os.ReadFile(privatePath)
	if err != nil {
		t.Fatal("read test signer")
	}
	key, err := internalrpcauth.ParsePrivateJWK(private)
	if err != nil || key.KeyID != artifactRetentionWorkerKeyID {
		t.Fatal("additive signer identity differs")
	}
	publicPath := filepath.Join(directory, "public.jwk")
	public, err := os.ReadFile(publicPath)
	if err != nil {
		t.Fatal("read test trust key")
	}
	derived, err := internalrpcauth.MarshalPublicJWK(key)
	if err != nil || !bytes.Equal(derived, public) {
		t.Fatal("additive private and public keys differ")
	}
	usernamePath := filepath.Join(directory, "postgresql-username")
	passwordPath := filepath.Join(directory, "postgresql-password")
	dsnPath := filepath.Join(directory, "postgresql-dsn")
	username, usernameErr := os.ReadFile(usernamePath)
	password, passwordErr := os.ReadFile(passwordPath)
	rawDSN, dsnErr := os.ReadFile(dsnPath)
	if usernameErr != nil || passwordErr != nil || dsnErr != nil || string(username) != artifactRetentionIssuerPrincipal {
		t.Fatal("additive issuer database material is unavailable")
	}
	if decoded, err := hex.DecodeString(string(password)); err != nil || len(decoded) != 32 {
		t.Fatal("additive issuer credential has an invalid format")
	}
	dsn, err := url.Parse(string(rawDSN))
	if err != nil || dsn.User == nil || dsn.User.Username() != artifactRetentionIssuerPrincipal ||
		dsn.Scheme != "postgresql" || dsn.Host != "internal-rpc-authority-postgresql-rw.kodex-system.svc.cluster.local:5432" ||
		dsn.Path != "/internal_rpc_authority" || dsn.Query().Get("sslmode") != "verify-full" ||
		dsn.Query().Get("sslrootcert") != "/var/run/config/kodex/internal-rpc-authority/postgresql/ca.pem" {
		t.Fatal("additive issuer DSN is outside the registered scope")
	}
	if received, exists := dsn.User.Password(); !exists || received != string(password) {
		t.Fatal("additive issuer DSN is not bound to the generated credential")
	}
	for _, path := range []string{privatePath, publicPath, usernamePath, passwordPath, dsnPath} {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			t.Fatal("additive key file is not private")
		}
	}
	if err := generateArtifactRetentionIssuerMaterial(directory); err == nil {
		t.Fatal("additive generation accepted an existing output")
	}
	retained, err := os.ReadFile(privatePath)
	if err != nil || !bytes.Equal(retained, private) {
		t.Fatal("additive generation changed the existing signer")
	}
}

// TestArtifactRetentionWorkerKeyRejectsAliasedParent closes generation under a symlink or directory with shared access.
// TestArtifactRetentionWorkerKeyRejectsAliasedParent запрещает генерацию через ссылку или каталог с общим доступом.
func TestArtifactRetentionWorkerKeyRejectsAliasedParent(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	actual := filepath.Join(base, "actual")
	if err := os.Mkdir(actual, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(actual, alias); err != nil {
		t.Fatal(err)
	}
	if err := generateArtifactRetentionIssuerMaterial(filepath.Join(alias, "issuer")); err == nil {
		t.Fatal("additive generation accepted an aliased parent")
	}
	if err := os.Chmod(actual, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := generateArtifactRetentionIssuerMaterial(filepath.Join(actual, "issuer")); err == nil {
		t.Fatal("additive generation accepted a shared parent")
	}
	if entries, err := os.ReadDir(actual); err != nil || len(entries) != 0 {
		t.Fatal("rejected generation wrote material")
	}
}
