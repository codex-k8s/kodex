package app

import (
	"crypto/x509"
	"errors"
	"os"
	"path/filepath"
)

const (
	// runtimeEgressProxyCAPath fixes the mounted public proxy CA input used to extend model-provider trust; it does not contain or select a private credential.
	// runtimeEgressProxyCAPath закрепляет mount-путь публичного CA прокси для расширения доверия к model-provider; не содержит и не выбирает приватные реквизиты.
	runtimeEgressProxyCAPath = "/var/run/config/kodex/runtime/egress-ca/ca.crt"
	// runtimeEgressTrustPath fixes the combined CA bundle published for provider processes, separately from their authentication files.
	// runtimeEgressTrustPath закрепляет путь общего CA bundle для provider-процессов отдельно от файлов авторизации.
	runtimeEgressTrustPath = "/var/run/config/kodex/runtime/egress-trust/ca-bundle.crt"
)

// systemTrustCandidates orders known OS CA bundle paths; selection stops at the first regular file and does not silently fall back after that file fails validation.
// systemTrustCandidates задаёт порядок известных OS CA bundle; выбирается первый обычный файл без молчаливого fallback после его неуспешной проверки.
var systemTrustCandidates = []string{
	"/etc/ssl/certs/ca-certificates.crt",
	"/etc/pki/tls/certs/ca-bundle.crt",
	"/etc/ssl/ca-bundle.pem",
	"/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem",
}

// materializeRuntimeEgressTrust selects the first regular system bundle and delegates combination with the fixed proxy CA/destination; unavailable candidates return a safe fixed error without changing global TLS configuration.
// materializeRuntimeEgressTrust выбирает первый обычный system bundle и передаёт объединение с фиксированными CA прокси и destination; отсутствие кандидатов даёт безопасную фиксированную ошибку без изменения глобального TLS.
func materializeRuntimeEgressTrust() error {
	for _, candidate := range systemTrustCandidates {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return materializeRuntimeEgressTrustFiles(candidate, runtimeEgressProxyCAPath, runtimeEgressTrustPath)
		}
	}
	return errors.New("runtime system trust bundle is unavailable")
}

// materializeRuntimeEgressTrustFiles reads nonempty system/proxy PEM inputs, requires each to contain a parseable certificate, rejects a noncanonical destination or current final symlink, then syncs and renames a temporary 0440 bundle. It assumes trusted parent mounts, does not bound ReadFile allocation and is not full chain validation or directory-fsync durability.
// materializeRuntimeEgressTrustFiles читает непустые system/proxy PEM, требует распознаваемый сертификат в каждом, отклоняет неканонический destination или текущую конечную symlink, затем синхронизирует и переименовывает временный bundle 0440. Предполагает доверенные parent mounts, не ограничивает выделение ReadFile и не доказывает полную проверку chain или durability через fsync каталога.
func materializeRuntimeEgressTrustFiles(systemPath, proxyPath, destination string) error {
	system, err := os.ReadFile(systemPath)
	if err != nil || len(system) == 0 {
		return errors.New("read runtime system trust bundle")
	}
	proxy, err := os.ReadFile(proxyPath)
	if err != nil || len(proxy) == 0 {
		return errors.New("read runtime proxy trust certificate")
	}
	systemPool, proxyPool := x509.NewCertPool(), x509.NewCertPool()
	if !systemPool.AppendCertsFromPEM(system) || !proxyPool.AppendCertsFromPEM(proxy) {
		return errors.New("runtime trust material is invalid")
	}
	if filepath.Clean(destination) != destination || filepath.Base(destination) != "ca-bundle.crt" {
		return errors.New("runtime trust destination is invalid")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
		return errors.New("create runtime trust directory")
	}
	if info, err := os.Lstat(destination); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("runtime trust destination is a symlink")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.New("inspect runtime trust destination")
	}
	bundle := make([]byte, 0, len(system)+len(proxy)+2)
	bundle = append(bundle, system...)
	if len(bundle) == 0 || bundle[len(bundle)-1] != '\n' {
		bundle = append(bundle, '\n')
	}
	bundle = append(bundle, proxy...)
	if bundle[len(bundle)-1] != '\n' {
		bundle = append(bundle, '\n')
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".ca-bundle-*")
	if err != nil {
		return errors.New("create runtime trust bundle")
	}
	temporaryName := temporary.Name()
	defer temporary.Close()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o440); err != nil {
		_ = temporary.Close()
		return errors.New("secure runtime trust bundle")
	}
	if _, err := temporary.Write(bundle); err != nil || temporary.Sync() != nil || temporary.Close() != nil {
		return errors.New("write runtime trust bundle")
	}
	if err := os.Rename(temporaryName, destination); err != nil {
		return errors.New("publish runtime trust bundle")
	}
	return nil
}
