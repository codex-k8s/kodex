package app

import (
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
)

func TestConfigRejectsAlternateSecurityBoundary(t *testing.T) {
	valid := Config{GRPCListen: ":8443", TechnicalListen: ":9090", ServerCertificateFile: "/tls.crt", ServerPrivateKeyFile: "/tls.key",
		ClientCAFile: "/ca.pem", WorkloadCertificateFile: "/workload.crt", WorkloadPrivateKeyFile: "/workload.key",
		DependencyCAFile: "/dependency-ca.pem", ApplicationGrantFile: "/grant", PolicyTarget: policyTarget,
		PolicyTLSServerName: policySNI, CredentialTarget: credentialTarget, CredentialTLSServerName: credentialSNI,
		ResolverTarget: resolverTarget, ResolverTLSServerName: resolverSNI, AuthorityVerifierSocket: authorityclient.VerifierSocketPath,
		AuthorityVerifierUID: 29002, AuthorityVerifierGID: 29000, AuthorityIssuerUID: 29001, AuthorityIssuerGID: 29000,
		RequestTimeout: 45 * time.Second, StartupTimeout: 30 * time.Second, ReadinessTimeout: 5 * time.Second, ShutdownTimeout: 20 * time.Second}
	if err := valid.validate(); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "другой policy target", mutate: func(config *Config) { config.PolicyTarget = "dns:///other:8443" }},
		{name: "другой credential SNI", mutate: func(config *Config) { config.CredentialTLSServerName = "other" }},
		{name: "другой resolver", mutate: func(config *Config) { config.ResolverTarget = "dns:///remote:8443" }},
		{name: "относительный grant", mutate: func(config *Config) { config.ApplicationGrantFile = "grant" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := valid
			test.mutate(&changed)
			if changed.validate() == nil {
				t.Fatal("небезопасная конфигурация принята")
			}
		})
	}
}
