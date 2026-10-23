package app

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestMaterializeRuntimeEgressTrustFilesCombinesSystemAndProxyRoots generates two disposable CA certificates and verifies both subjects survive publication with exact mode 0440; no live endpoint or private credential is contacted.
// TestMaterializeRuntimeEgressTrustFilesCombinesSystemAndProxyRoots создаёт два одноразовых CA-сертификата и проверяет сохранение обоих subjects после публикации с точным режимом 0440; живые endpoints и приватные реквизиты не используются.
func TestMaterializeRuntimeEgressTrustFilesCombinesSystemAndProxyRoots(t *testing.T) {
	directory := t.TempDir()
	systemPath, proxyPath := filepath.Join(directory, "system.pem"), filepath.Join(directory, "proxy.pem")
	system, systemCertificate := testRootCertificate(t, "system", 1)
	proxy, proxyCertificate := testRootCertificate(t, "proxy", 2)
	if err := os.WriteFile(systemPath, system, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(proxyPath, proxy, 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(directory, "trust", "ca-bundle.crt")
	if err := materializeRuntimeEgressTrustFiles(systemPath, proxyPath, destination); err != nil {
		t.Fatal(err)
	}
	bundle, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(bundle) || !poolContainsSubject(pool, systemCertificate.RawSubject) || !poolContainsSubject(pool, proxyCertificate.RawSubject) {
		t.Fatal("combined runtime trust bundle lost a root")
	}
	info, _ := os.Stat(destination)
	if info.Mode().Perm() != 0o440 {
		t.Fatalf("runtime trust bundle mode = %o", info.Mode().Perm())
	}
}

// poolContainsSubject compares a synthetic certificate's raw subject against pool subjects for unit assertions; it is not a certificate-chain or hostname verifier.
// poolContainsSubject сравнивает raw subject синтетического сертификата с subjects пула для unit-проверок; не проверяет certificate chain или hostname.
func poolContainsSubject(pool *x509.CertPool, subject []byte) bool {
	for _, current := range pool.Subjects() {
		if bytes.Equal(current, subject) {
			return true
		}
	}
	return false
}

// testRootCertificate creates and parses a short-lived self-signed P-256 test CA, returning public PEM and certificate metadata while keeping its key only in local test memory.
// testRootCertificate создаёт и разбирает краткоживущий self-signed P-256 тестовый CA, возвращая публичный PEM и метаданные сертификата; ключ остаётся лишь в памяти теста.
func testRootCertificate(t *testing.T, name string, serial int64) ([]byte, *x509.Certificate) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	template := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: name}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, KeyUsage: x509.KeyUsageCertSign, BasicConstraintsValid: true}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(raw)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}), certificate
}
