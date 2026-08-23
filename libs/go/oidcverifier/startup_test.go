package oidcverifier

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestVerifierStartsFailClosedWhenInitialJWKSIsUnavailable(t *testing.T) {
	t.Parallel()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate test CA key: %v", err)
	}
	now := time.Now().UTC()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "OIDC test CA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	certificate, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("create test CA: %v", err)
	}
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate}), 0o600); err != nil {
		t.Fatalf("write test CA: %v", err)
	}

	verifier, err := New(t.Context(), Config{
		Issuer: "https://issuer.test", Audience: "mattercodex-test", JWKSURL: "https://issuer.test/jwks",
		ConnectAddress: "localhost:443", TLSServerName: "issuer.test", CAFile: caFile, Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("initial JWKS outage stopped verifier startup: %v", err)
	}
	defer verifier.Close()
	if _, err := verifier.VerifyToken(t.Context(), "header.payload.signature"); !errors.Is(err, ErrSigningKeysUnavailable) {
		t.Fatalf("fail-closed verifier returned %v, expected typed unavailable", err)
	}
}
