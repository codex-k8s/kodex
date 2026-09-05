package app

import (
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/integration/provider/openai"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
)

func TestConfigRejectsAlternateSecurityBoundary(t *testing.T) {
	valid := Config{
		Egress:     openai.EgressConfig{Revision: "test-revision", Digest: strings.Repeat("a", 64)},
		GRPCListen: ":8443", TechnicalListen: ":9090", SpoolDirectory: "/spool",
		ServerCertificateFile: "/tls.crt", ServerPrivateKeyFile: "/tls.key", ClientCAFile: "/ca.pem",
		WorkloadCertificateFile: "/workload.crt", WorkloadPrivateKeyFile: "/workload.key", DependencyCAFile: "/dependency-ca.pem",
		PolicyTarget: policyTarget, PolicyTLSServerName: policySNI,
		CredentialTarget: credentialTarget, CredentialTLSServerName: credentialSNI,
		AuthorityVerifierSocket: authorityclient.VerifierSocketPath, AuthorityVerifierUID: 29002, AuthorityVerifierGID: 29000,
		AuthorityIssuerSocket: authorityclient.IssuerSocketPath, AuthorityIssuerUID: 29001, AuthorityIssuerGID: 29000,
		RequestTimeout: requestTimeout, StartupTimeout: startupTimeout, ReadinessTimeout: readinessTimeout, ShutdownTimeout: shutdownTimeout,
	}
	if err := valid.validate(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "missing egress revision", mutate: func(config *Config) { config.Egress.Revision = "" }},
		{name: "invalid egress digest", mutate: func(config *Config) { config.Egress.Digest = "invalid" }},
		{name: "другой policy target", mutate: func(config *Config) { config.PolicyTarget = "dns:///other:8443" }},
		{name: "другой credential SNI", mutate: func(config *Config) { config.CredentialTLSServerName = "other" }},
		{name: "другой issuer UID", mutate: func(config *Config) { config.AuthorityIssuerUID = 1 }},
		{name: "относительный spool", mutate: func(config *Config) { config.SpoolDirectory = "spool" }},
		{name: "другой request timeout", mutate: func(config *Config) { config.RequestTimeout++ }},
		{name: "другой shutdown timeout", mutate: func(config *Config) { config.ShutdownTimeout-- }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := valid
			test.mutate(&changed)
			if changed.validate() == nil {
				t.Fatal("небезопасная конфигурация принята")
			}
		})
	}
}

func TestLoadConfigRequiresDeploymentEgressExpectations(t *testing.T) {
	t.Setenv("STT_EGRESS_EXPECTED_REVISION", "")
	t.Setenv("STT_EGRESS_EXPECTED_DIGEST", "")
	if _, err := loadConfig(); err == nil {
		t.Fatal("missing deployment expectations accepted")
	}
	t.Setenv("STT_EGRESS_EXPECTED_REVISION", "test-generation")
	t.Setenv("STT_EGRESS_EXPECTED_DIGEST", strings.Repeat("a", 64))
	config, err := loadConfig()
	if err != nil || config.Egress.Revision != "test-generation" || config.Egress.Digest != strings.Repeat("a", 64) {
		t.Fatal("typed egress configuration not loaded")
	}
}
