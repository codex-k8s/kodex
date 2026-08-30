package app

import "testing"

func TestProviderCredentialBoundarySeparatesResolverAndTarget(t *testing.T) {
	t.Parallel()
	valid := Config{
		ClientCAFile:                  "/var/run/config/kodex/control-plane/internal-ca/ca.pem",
		SecretBrokerTarget:            providerTarget,
		SecretBrokerTLSServerName:     providerTLSServerName,
		ProviderResolverTarget:        providerAuthorityResolverTarget,
		ProviderResolverTLSServerName: providerAuthorityResolverSNI,
		ProviderResolverCAFile:        "/var/run/config/kodex/control-plane/internal-ca/ca.pem",
	}
	if !validProviderCredentialBoundary(valid) {
		t.Fatal("exact provider credential boundary was rejected")
	}
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "provider target", mutate: func(config *Config) { config.SecretBrokerTarget = providerAuthorityResolverTarget }},
		{name: "provider SNI", mutate: func(config *Config) { config.SecretBrokerTLSServerName = providerAuthorityResolverSNI }},
		{name: "resolver target", mutate: func(config *Config) { config.ProviderResolverTarget = providerTarget }},
		{name: "resolver SNI", mutate: func(config *Config) { config.ProviderResolverTLSServerName = providerTLSServerName }},
		{name: "resolver CA", mutate: func(config *Config) { config.ProviderResolverCAFile = "/tmp/foreign-ca.pem" }},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			changed := valid
			test.mutate(&changed)
			if validProviderCredentialBoundary(changed) {
				t.Fatal("non-canonical provider credential boundary was accepted")
			}
		})
	}
}
