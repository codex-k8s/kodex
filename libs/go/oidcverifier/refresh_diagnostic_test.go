package oidcverifier

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

// refreshTransport supplies a controlled HTTP outcome through the real http.Client.Do wrapping path.
// RU: refreshTransport задаёт управляемый HTTP-исход через реальную обёртку http.Client.Do.
type refreshTransport func(*http.Request) (*http.Response, error)

// RoundTrip forwards the fixture outcome without contacting an external issuer.
// RU: RoundTrip передаёт исход fixture без обращения к внешнему issuer.
func (transport refreshTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

// TestRefreshDiagnosticDiscardsNativeCause proves initial typed causes remain distinguishable without retaining secret-bearing transport errors.
// RU: TestRefreshDiagnosticDiscardsNativeCause доказывает различимость начальных причин без сохранения чувствительных ошибок транспорта.
func TestRefreshDiagnosticDiscardsNativeCause(t *testing.T) {
	const privateMarker = "PRIVATE_JWKS_FIXTURE"
	cases := []struct {
		name  string
		err   error
		class string
	}{
		{"cancel", context.Canceled, "canceled"},
		{"deadline", context.DeadlineExceeded, "deadline"},
		{"dns", &net.DNSError{Err: privateMarker, Name: privateMarker}, "dns"},
		{"network", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New(privateMarker)}, "network"},
		{"tls", x509.UnknownAuthorityError{}, "tls_verification"},
		{"unknown", errors.New(privateMarker), "transport_unknown"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			set := newBoundedKeySet(&http.Client{Transport: refreshTransport(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, test.err
			})}, "https://issuer.invalid/"+privateMarker)
			err := set.Refresh(t.Context())
			if err == nil || err.Error() != "fetch OIDC JWKS" {
				t.Fatal("historical public error changed")
			}
			if strings.Contains(fmt.Sprintf("%+v %#v", err, err), privateMarker) || errors.Unwrap(err) != nil || errors.Is(err, test.err) {
				t.Fatal("refresh retained native error details")
			}
			verifier := &Verifier{keys: set}
			if got := verifier.SigningKeyFailureClass(); got != test.class {
				t.Fatalf("class=%s expected=%s", got, test.class)
			}
			if calls != 1 || !errors.Is(set.verificationError(), ErrSigningKeysUnavailable) {
				t.Fatal("diagnostic fetched again or granted token authority")
			}
		})
	}
}

// TestRefreshDiagnosticPreservesFatalAndRecoveryPolicy keeps transient recovery and permanent blocking unchanged while retaining only closed classes.
// RU: TestRefreshDiagnosticPreservesFatalAndRecoveryPolicy сохраняет восстановление после временного отказа и постоянную блокировку, удерживая только закрытые классы.
func TestRefreshDiagnosticPreservesFatalAndRecoveryPolicy(t *testing.T) {
	valid := string(testJWKS(t, "diagnostic-key"))
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		current := status
		set := newBoundedKeySet(&http.Client{Transport: refreshTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: current, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(valid))}, nil
		})}, "https://issuer.invalid/jwks")
		if set.Refresh(t.Context()) == nil {
			t.Fatal("HTTP refusal was accepted")
		}
		verifier := &Verifier{keys: set}
		before := verifier.SigningKeyFailureClass()
		if before == "" {
			t.Fatal("initial refusal lost its class")
		}
		current = http.StatusOK
		err := set.Refresh(t.Context())
		if status == http.StatusForbidden {
			if err == nil || verifier.SigningKeyFailureClass() != before || !errors.Is(set.verificationError(), ErrSigningKeysUnavailable) {
				t.Fatal("fatal failure recovered automatically")
			}
		} else if err != nil || verifier.SigningKeyFailureClass() != "" || set.verificationError() != nil {
			t.Fatal("transient recovery changed")
		}
	}
}
