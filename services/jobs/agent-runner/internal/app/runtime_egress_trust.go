package app

import (
	"crypto/x509"
	"errors"
	"os"
	"path/filepath"
)

const (
	runtimeEgressProxyCAPath = "/var/run/config/kodex/runtime/egress-ca/ca.crt"
	runtimeEgressTrustPath   = "/var/run/config/kodex/runtime/egress-trust/ca-bundle.crt"
)

var systemTrustCandidates = []string{
	"/etc/ssl/certs/ca-certificates.crt",
	"/etc/pki/tls/certs/ca-bundle.crt",
	"/etc/ssl/ca-bundle.pem",
	"/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem",
}

func materializeRuntimeEgressTrust() error {
	for _, candidate := range systemTrustCandidates {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return materializeRuntimeEgressTrustFiles(candidate, runtimeEgressProxyCAPath, runtimeEgressTrustPath)
		}
	}
	return errors.New("runtime system trust bundle is unavailable")
}

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
