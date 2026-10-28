package errorclass

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
)

// TLS recognizes typed certificate verification failures without formatting
// certificate, hostname or error content. Unknown failures remain unknown;
// callers own SDK-specific precedence and retry decisions.
// TLS распознаёт типизированные ошибки проверки сертификата без форматирования
// сертификата, имени узла либо сообщения. Неизвестный отказ остаётся unknown;
// приоритет SDK и решения о повторе принадлежат вызывающему.
func TLS(err error) string {
	var unknownCA x509.UnknownAuthorityError
	var invalidCertificate x509.CertificateInvalidError
	var hostname x509.HostnameError
	var verification *tls.CertificateVerificationError
	if errors.As(err, &unknownCA) || errors.As(err, &invalidCertificate) || errors.As(err, &hostname) || errors.As(err, &verification) {
		return "tls_verification"
	}
	return "unknown"
}
