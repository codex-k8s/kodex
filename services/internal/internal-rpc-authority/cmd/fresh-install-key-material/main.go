package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
)

// artifactRetentionWorkerKeyID binds fresh and additive provisioning to the same registered signing identity.
// artifactRetentionWorkerKeyID связывает новую установку и добавление издателя с одной зарегистрированной идентичностью подписанта.
const artifactRetentionWorkerKeyID = "artifact-retention-platform-worker-g1"

// artifactRetentionIssuerPrincipal is the exact new LOGIN registered by the authority issuer migration.
// artifactRetentionIssuerPrincipal — точный новый LOGIN, зарегистрированный миграцией издателя authority.
const artifactRetentionIssuerPrincipal = "ira_artifact_retention_issuer_g1"

// artifactRetentionIssuerPostgresSuffix keeps the issuer on the existing exact verify-full service and CA.
// artifactRetentionIssuerPostgresSuffix закрепляет за издателем прежний точный сервис и CA для verify-full.
const artifactRetentionIssuerPostgresSuffix = "@internal-rpc-authority-postgresql-rw.kodex-system.svc.cluster.local:5432/internal_rpc_authority?sslmode=verify-full&sslrootcert=/var/run/config/kodex/internal-rpc-authority/postgresql/ca.pem"

// main chooses the full fresh-install registry or exclusive local material for the artifact-retention issuer.
// The additive scope generates only the new worker signer and issuer database credential. It performs no Kubernetes, database or rotation operations.
// main выбирает полный реестр новой установки либо отдельный локальный материал издателя artifact-retention.
// Дополнительный режим создаёт только новый worker signer и реквизиты роли издателя. Kubernetes, PostgreSQL и ротацию он не меняет.
func main() {
	if len(os.Args) == 4 && os.Args[1] == "--scope" && os.Args[2] == "artifact-retention-issuer" {
		if err := generateArtifactRetentionIssuerMaterial(os.Args[3]); err != nil {
			fatal("generate artifact-retention issuer material", err)
		}
		return
	}
	if len(os.Args) != 2 || os.Args[1] == "" {
		_, _ = fmt.Fprintln(os.Stderr, "fresh install key output directory is required")
		os.Exit(64)
	}
	output := os.Args[1]
	for _, item := range []struct {
		name string
		kid  string
	}{
		{"publisher/restore-signer", "ira-publisher-restore-g1"},
		{"publisher/readback-signer", "ira-publisher-readback-g1"},
		{"publisher/manifest-signer", "ira-publisher-manifest-g1"},
		{"restore/pitr-evidence", "ira-restore-pitr-evidence-g1"},
		{"platform-worker/automation-scheduler", "automation-scheduler-platform-worker-g1"},
		{"platform-worker/session-archive", "session-archive-platform-worker-g1"},
		{"platform-worker/artifact-retention", artifactRetentionWorkerKeyID},
		{"platform-worker/integration-gateway", "integration-gateway-platform-worker-g1"},
		{"platform-worker/interaction-gateway", "interaction-gateway-platform-worker-g1"},
		{"platform-worker/email-bridge", "email-bridge-platform-worker-g1"},
		{"platform-worker/runtime-controller", "runtime-controller-platform-worker-g1"},
		{"platform-worker/role-image-builder", "role-image-builder-platform-worker-g1"},
		{"platform-worker/image-admission", "image-admission-platform-worker-g1"},
		{"platform-worker/image-promotion", "image-promotion-platform-worker-g1"},
		{"platform-worker/secret-broker", "secret-broker-platform-worker-g1"},
		{"platform-worker/control-plane", "control-plane-platform-worker-g1"},
	} {
		directory := filepath.Join(output, item.name)
		if err := os.MkdirAll(directory, 0o700); err != nil {
			fatal("create key directory", err)
		}
		if err := writeWorkerKeyPair(item.kid, func(name string, data []byte) error {
			return os.WriteFile(filepath.Join(directory, name), data, 0o600)
		}); err != nil {
			fatal("write key pair", err)
		}
	}
}

// generateArtifactRetentionIssuerMaterial creates the scoped signer and LOGIN files in a new owned directory without following aliases or replacing old material.
// generateArtifactRetentionIssuerMaterial создаёт файлы подписанта и LOGIN в новом собственном каталоге без перехода по ссылкам и перезаписи прежнего материала.
func generateArtifactRetentionIssuerMaterial(output string) error {
	if !filepath.IsAbs(output) || filepath.Clean(output) != output || output == "/" {
		return errors.New("exact private output path is required")
	}
	parent := filepath.Dir(output)
	resolved, err := filepath.EvalSymlinks(parent)
	if err != nil || resolved != parent {
		return errors.New("worker key parent path is not canonical")
	}
	info, err := os.Lstat(parent)
	if err != nil {
		return fmt.Errorf("inspect worker key parent: %w", err)
	}
	owner, valid := info.Sys().(*syscall.Stat_t)
	if !valid || int(owner.Uid) != os.Geteuid() || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return errors.New("worker key parent is not a private owned directory")
	}
	parentRoot, err := os.OpenRoot(parent)
	if err != nil {
		return fmt.Errorf("open worker key parent: %w", err)
	}
	defer parentRoot.Close()
	opened, err := parentRoot.Stat(".")
	if err != nil || !os.SameFile(opened, info) || opened.Mode().Perm() != 0o700 {
		return errors.New("worker key parent identity changed")
	}
	name := filepath.Base(output)
	if err := parentRoot.Mkdir(name, 0o700); err != nil {
		return fmt.Errorf("reserve new worker key directory: %w", err)
	}
	created, err := parentRoot.Lstat(name)
	if err != nil || !created.IsDir() || created.Mode().Perm() != 0o700 {
		return errors.New("new worker key directory is not private")
	}
	target, err := parentRoot.OpenRoot(name)
	if err != nil {
		return fmt.Errorf("open worker key directory: %w", err)
	}
	defer target.Close()
	opened, err = target.Stat(".")
	if err != nil || !os.SameFile(opened, created) || opened.Mode().Perm() != 0o700 {
		return errors.New("worker key directory identity changed")
	}
	if err := writeWorkerKeyPair(artifactRetentionWorkerKeyID, func(name string, material []byte) error {
		return writeNewWorkerKeyFile(target, name, material)
	}); err != nil {
		return err
	}
	if err := writeArtifactRetentionDatabaseMaterial(target); err != nil {
		return err
	}
	if err := syncWorkerKeyDirectory(target); err != nil {
		return err
	}
	return syncWorkerKeyDirectory(parentRoot)
}

// writeArtifactRetentionDatabaseMaterial creates a new scoped PostgreSQL password and derives the DSN without emitting credential bytes.
// writeArtifactRetentionDatabaseMaterial создаёт отдельный пароль PostgreSQL и выводит из него DSN без публикации реквизитов.
func writeArtifactRetentionDatabaseMaterial(root *os.Root) error {
	var entropy [32]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return fmt.Errorf("generate issuer database credential: %w", err)
	}
	defer clear(entropy[:])
	var password [64]byte
	hex.Encode(password[:], entropy[:])
	defer clear(password[:])
	dsn := append([]byte("postgresql://"+artifactRetentionIssuerPrincipal+":"), password[:]...)
	dsn = append(dsn, artifactRetentionIssuerPostgresSuffix...)
	defer clear(dsn)
	for _, file := range []struct {
		name string
		data []byte
	}{
		{"postgresql-username", []byte(artifactRetentionIssuerPrincipal)},
		{"postgresql-password", password[:]},
		{"postgresql-dsn", dsn},
	} {
		if err := writeNewWorkerKeyFile(root, file.name, file.data); err != nil {
			return err
		}
	}
	return nil
}

// writeWorkerKeyPair generates the registered ES256 JWK pair and writes the same representation in both provisioning modes.
// writeWorkerKeyPair создаёт зарегистрированную пару ES256 JWK и записывает одинаковое представление в обоих режимах подготовки.
func writeWorkerKeyPair(keyID string, write func(string, []byte) error) error {
	key, err := internalrpcauth.GenerateES256Key(keyID)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}
	privateJWK, err := internalrpcauth.MarshalPrivateJWK(key)
	if err != nil {
		return fmt.Errorf("marshal private JWK: %w", err)
	}
	publicJWK, err := internalrpcauth.MarshalPublicJWK(key)
	if err != nil {
		return fmt.Errorf("marshal public JWK: %w", err)
	}
	for _, output := range []struct {
		name string
		data []byte
	}{{"private.jwk", privateJWK}, {"public.jwk", publicJWK}} {
		if err := write(output.name, output.data); err != nil {
			return err
		}
	}
	return nil
}

// writeNewWorkerKeyFile writes an exclusive private file and synchronizes its bytes before returning.
// writeNewWorkerKeyFile создаёт закрытый файл без перезаписи и синхронизирует его байты перед возвратом.
func writeNewWorkerKeyFile(root *os.Root, name string, material []byte) error {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, operationErr := file.Write(material)
	if operationErr == nil {
		operationErr = file.Sync()
	}
	return errors.Join(operationErr, file.Close())
}

// syncWorkerKeyDirectory makes new records durable within a previously verified root.
// syncWorkerKeyDirectory закрепляет новые записи каталога внутри ранее проверенного корня.
func syncWorkerKeyDirectory(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}

// fatal writes the failed fresh-key operation and its returned error to stderr, then exits with status one. It does not retry, roll back previously written keys or serialize key bytes itself.
// fatal выводит неудачную операцию создания ключей и возвращённую ошибку в stderr и завершает процесс с кодом один. Повтора, отката уже записанных ключей или самостоятельной сериализации байтов ключа нет.
func fatal(operation string, err error) {
	_, _ = fmt.Fprintf(os.Stderr, "fresh install key generation failed: %s: %v\n", operation, err)
	os.Exit(1)
}
