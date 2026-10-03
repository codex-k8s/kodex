package gateway

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"strings"
	"time"
)

// TLSInterceptAuthority выпускает короткоживущий leaf только для exact SNI,
// уже разрешённого execution-scoped policy. Dedicated CA не используется ни
// для workload identity, ни для межсервисного mTLS.
type TLSInterceptAuthority struct {
	certificate *x509.Certificate
	privateKey  crypto.Signer
	leafKey     crypto.Signer
}

func NewTLSInterceptAuthority(certificatePEM, privateKeyPEM []byte) (*TLSInterceptAuthority, error) {
	block, _ := pem.Decode(certificatePEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("runtime proxy CA certificate is invalid")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil || !certificate.IsCA || time.Until(certificate.NotAfter) < time.Hour {
		return nil, errors.New("runtime proxy CA certificate is invalid")
	}
	keyBlock, _ := pem.Decode(privateKeyPEM)
	if keyBlock == nil {
		return nil, errors.New("runtime proxy CA private key is invalid")
	}
	privateKey, err := parseSigner(keyBlock.Bytes)
	publicKey, ok := certificate.PublicKey.(*ecdsa.PublicKey)
	if err != nil || !ok || !publicKey.Equal(privateKey.Public()) {
		return nil, errors.New("runtime proxy CA private key is invalid")
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, errors.New("generate runtime proxy leaf key")
	}
	return &TLSInterceptAuthority{certificate: certificate, privateKey: privateKey, leafKey: leafKey}, nil
}

func parseSigner(raw []byte) (crypto.Signer, error) {
	if key, err := x509.ParsePKCS8PrivateKey(raw); err == nil {
		if signer, ok := key.(crypto.Signer); ok {
			return signer, nil
		}
	}
	if key, err := x509.ParseECPrivateKey(raw); err == nil {
		return key, nil
	}
	return nil, errors.New("private key is not a supported signer")
}

func (authority *TLSInterceptAuthority) certificateFor(hostname string) (tls.Certificate, error) {
	if authority == nil || hostname == "" || strings.ContainsAny(hostname, "*:/ ") {
		return tls.Certificate{}, errors.New("runtime proxy leaf hostname is invalid")
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return tls.Certificate{}, errors.New("generate runtime proxy leaf serial")
	}
	now := time.Now()
	notAfter := now.Add(24 * time.Hour)
	if authority.certificate.NotAfter.Before(notAfter) {
		notAfter = authority.certificate.NotAfter
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: hostname},
		DNSNames:              []string{hostname},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	raw, err := x509.CreateCertificate(rand.Reader, template, authority.certificate, authority.leafKey.Public(), authority.privateKey)
	if err != nil {
		return tls.Certificate{}, errors.New("issue runtime proxy leaf certificate")
	}
	return tls.Certificate{Certificate: [][]byte{raw, authority.certificate.Raw}, PrivateKey: authority.leafKey}, nil
}
