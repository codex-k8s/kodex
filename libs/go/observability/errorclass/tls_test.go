package errorclass

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"syscall"
	"testing"
)

// TestTLSRecognizesOnlyTypedVerificationEvidence keeps generic network and
// arbitrary text unknown, while preserving the four existing CP typed cases.
// TestTLSRecognizesOnlyTypedVerificationEvidence оставляет общую сеть и текст
// неизвестными, сохраняя четыре прежних типизированных случая control-plane.
func TestTLSRecognizesOnlyTypedVerificationEvidence(t *testing.T) {
	for _, cause := range []error{x509.UnknownAuthorityError{}, x509.CertificateInvalidError{}, x509.HostnameError{Host: "PRIVATE_HOST"}, &tls.CertificateVerificationError{Err: errors.New("PRIVATE_CA")}} {
		if TLS(fmt.Errorf("private wrapper: %w", cause)) != "tls_verification" {
			t.Fatal("typed verification evidence lost")
		}
		if TLS(errors.Join(syscall.ECONNREFUSED, cause)) != "tls_verification" {
			t.Fatal("network wrapper hid typed verification evidence")
		}
	}
	for _, cause := range []error{nil, syscall.ECONNREFUSED, errors.New("x509: certificate PRIVATE_HOST is not valid")} {
		if TLS(cause) != "unknown" {
			t.Fatal("untyped text became TLS evidence")
		}
	}
}
