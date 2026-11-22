package app

import "testing"

// TestKubernetesIdentitySetupConfigurationIsCompleteOrAbsent excludes partially deployed plaintext lanes.
// Private material cannot share runtime/draft/platform storage or an existing broker service listener.
// TestKubernetesIdentitySetupConfigurationIsCompleteOrAbsent исключает частично установленные plaintext lanes.
// Private material не делит runtime/draft/platform storage либо existing broker service listener.
func TestKubernetesIdentitySetupConfigurationIsCompleteOrAbsent(t *testing.T) {
	if (Config{}).validateKubernetesIdentityUpload() != nil {
		t.Fatal("absent optional identity upload lane changed the existing deployment")
	}
	valid := Config{RuntimeNamespace: "kodex-runtime", DraftNamespace: "kodex-secret-drafts", GRPCListen: ":8443", TechnicalListen: ":9090",
		KubernetesIdentityNamespace: "kodex-integration-identities", KubernetesUploadListen: ":9444", KubernetesUploadOrigin: "https://broker.example.test:9444",
		KubernetesUploadCertificateFile: "/var/run/secrets/kodex/kubernetes-upload/tls.crt", KubernetesUploadPrivateKeyFile: "/var/run/secrets/kodex/kubernetes-upload/tls.key"}
	if valid.validateKubernetesIdentityUpload() != nil {
		t.Fatal("complete purpose-only TLS configuration was rejected")
	}
	for _, change := range []func(*Config){
		func(config *Config) { config.KubernetesUploadCertificateFile = "" },
		func(config *Config) { config.KubernetesUploadPrivateKeyFile = "relative.key" },
		func(config *Config) { config.KubernetesIdentityNamespace = config.RuntimeNamespace },
		func(config *Config) { config.KubernetesIdentityNamespace = config.DraftNamespace },
		func(config *Config) { config.KubernetesIdentityNamespace = "kodex-system" },
		func(config *Config) { config.KubernetesUploadOrigin = "http://broker.example.test:9444" },
		func(config *Config) { config.KubernetesUploadOrigin += "?target=foreign" },
		func(config *Config) { config.KubernetesUploadOrigin = "https://user:password@broker.example.test:9444" },
		func(config *Config) { config.KubernetesUploadListen = config.GRPCListen },
	} {
		changed := valid
		change(&changed)
		if changed.validateKubernetesIdentityUpload() == nil {
			t.Fatal("partial, foreign or plaintext purpose configuration was accepted")
		}
	}
}
