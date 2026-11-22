package kubernetestarget

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"time"
)

// credentialOutputDeadline intersects short output credit with every known trust/client certificate lifetime.
// Opaque bearer expiry is not guessed; selected authentication must still be observed at the target.
// credentialOutputDeadline пересекает short output credit со сроками всех known trust/client certificates.
// Opaque bearer expiry не угадывается; selected authentication проверяется на target отдельно.
func credentialOutputDeadline(credential Credential, now, limit time.Time, extraAuthorities ...[]byte) (time.Time, error) {
	if now.IsZero() || !limit.After(now) {
		return time.Time{}, ErrPrivateIdentityRejected
	}
	deadline := now.Add(5 * time.Second)
	if limit.Before(deadline) {
		deadline = limit
	}
	for _, bundle := range append([][]byte{credential.ca}, extraAuthorities...) {
		if !validAuthority(bundle, now) {
			return time.Time{}, ErrPrivateIdentityRejected
		}
		for remaining := bytes.TrimSpace(bundle); len(remaining) > 0; {
			block, rest := pem.Decode(remaining)
			if block == nil {
				return time.Time{}, ErrPrivateIdentityRejected
			}
			certificate, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return time.Time{}, ErrPrivateIdentityRejected
			}
			if certificate.NotAfter.Before(deadline) {
				deadline = certificate.NotAfter
			}
			remaining = bytes.TrimSpace(rest)
		}
	}
	for _, der := range credential.cert.Certificate {
		certificate, err := x509.ParseCertificate(der)
		if err != nil || now.Before(certificate.NotBefore) || !now.Before(certificate.NotAfter) {
			return time.Time{}, ErrPrivateIdentityRejected
		}
		if certificate.NotAfter.Before(deadline) {
			deadline = certificate.NotAfter
		}
	}
	return deadline, nil
}
