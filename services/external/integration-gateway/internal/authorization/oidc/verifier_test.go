package oidc

import (
	"errors"
	"net/http"
	"testing"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (function roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestExactTransportRejectsChangedDestination(t *testing.T) {
	t.Parallel()
	called := false
	transport := exactTransport{host: "sso.kodex.works", next: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("unexpected request")
	})}
	for _, endpoint := range []string{
		"http://sso.kodex.works/.well-known/openid-configuration",
		"https://other.mattercodex.local/jwks",
	} {
		request, err := http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			t.Fatalf("create request: %v", err)
		}
		if _, err := transport.RoundTrip(request); err == nil {
			t.Fatalf("endpoint %q was accepted", endpoint)
		}
	}
	if called {
		t.Fatal("rejected request reached the underlying transport")
	}
}

func TestExactTransportAllowsIssuerHost(t *testing.T) {
	t.Parallel()
	expected := errors.New("transport reached")
	transport := exactTransport{host: "sso.kodex.works", next: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, expected
	})}
	request, err := http.NewRequest(http.MethodGet, "https://sso.kodex.works/jwks", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if _, err := transport.RoundTrip(request); !errors.Is(err, expected) {
		t.Fatalf("underlying transport error = %v, want %v", err, expected)
	}
}
