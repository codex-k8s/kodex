package main

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"testing"
)

func TestProbeRejectsForeignTargetBeforeNetwork(t *testing.T) {
	for _, host := range []string{"localhost", "api.openai.com", "chatgpt.com:443", "chatgpt.com.evil.example", ""} {
		x := probeHost(t.Context(), host)
		if x.Failure != "TARGET_REJECTED" || x.TransportOK || x.ConnectStatus != 0 || x.HTTPStatus != 0 {
			t.Fatal("probe accepted a foreign destination")
		}
	}
}

func TestProbeFailureUsesClosedCodes(t *testing.T) {
	for _, test := range []struct {
		err      error
		expected string
	}{
		{context.DeadlineExceeded, "TIMEOUT"},
		{&net.DNSError{Err: "synthetic-private-payload", Name: "synthetic-private-payload"}, "DNS"},
		{x509.UnknownAuthorityError{}, "TLS_CERTIFICATE"},
		{errors.New("synthetic-private-payload"), "TRANSPORT"},
	} {
		if probeFailure(test.err) != test.expected {
			t.Fatal("probe diagnostic boundary changed")
		}
	}
}
