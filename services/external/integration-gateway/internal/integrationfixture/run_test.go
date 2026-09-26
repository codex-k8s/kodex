package integrationfixture

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestConfiguredListenAddress(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{name: "default", want: defaultListenAddress},
		{name: "ephemeral loopback", value: "127.0.0.1:0", want: "127.0.0.1:0"},
		{name: "fixed loopback", value: "127.0.0.1:18083", want: "127.0.0.1:18083"},
		{name: "wildcard is forbidden", value: "0.0.0.0:18083", wantErr: true},
		{name: "privileged port is forbidden", value: "127.0.0.1:443", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(listenAddressEnv, test.value)
			got, err := configuredListenAddress()
			if test.wantErr {
				if err == nil {
					t.Fatalf("configuredListenAddress() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("configuredListenAddress() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("configuredListenAddress() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestConfiguredTLSServer(t *testing.T) {
	tokenDigest := sha256.Sum256([]byte("fixture-token-with-at-least-thirty-two-bytes"))
	digest := hex.EncodeToString(tokenDigest[:])
	tests := []struct {
		name        string
		address     string
		certificate string
		privateKey  string
		digest      string
		wantNil     bool
		wantErr     bool
	}{
		{name: "disabled", wantNil: true},
		{name: "cluster listener", address: ":8443", certificate: "/tls/tls.crt", privateKey: "/tls/tls.key", digest: digest},
		{name: "loopback listener", address: "127.0.0.1:18443", certificate: "/tls/tls.crt", privateKey: "/tls/tls.key", digest: digest},
		{name: "partial", address: ":8443", wantErr: true},
		{name: "wildcard address", address: "0.0.0.0:8443", certificate: "/tls/tls.crt", privateKey: "/tls/tls.key", digest: digest, wantErr: true},
		{name: "relative certificate", address: ":8443", certificate: "tls.crt", privateKey: "/tls/tls.key", digest: digest, wantErr: true},
		{name: "uppercase digest", address: ":8443", certificate: "/tls/tls.crt", privateKey: "/tls/tls.key", digest: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(tlsListenAddressEnv, test.address)
			t.Setenv(tlsCertificateFileEnv, test.certificate)
			t.Setenv(tlsPrivateKeyFileEnv, test.privateKey)
			t.Setenv(bearerTokenSHA256Env, test.digest)
			got, err := configuredTLSServer()
			if test.wantErr {
				if err == nil {
					t.Fatalf("configuredTLSServer() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("configuredTLSServer() error = %v", err)
			}
			if test.wantNil {
				if got != nil {
					t.Fatalf("configuredTLSServer() = %#v, want nil", got)
				}
				return
			}
			if got == nil || got.listenAddress != test.address || got.certificateFile != test.certificate ||
				got.privateKeyFile != test.privateKey || got.bearerDigest != tokenDigest {
				t.Fatalf("configuredTLSServer() = %#v", got)
			}
		})
	}
}
