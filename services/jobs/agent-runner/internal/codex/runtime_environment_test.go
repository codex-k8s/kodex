package codex

import (
	"testing"
)

// syntheticRuntimeProxy is a nonworking fixture credential for exact URL-shape checks, not a live token.
// syntheticRuntimeProxy задаёт нерабочие реквизиты fixture для проверки формы URL, а не живой токен.
const syntheticRuntimeProxy = "http://kodex:fixture.signature@egress-gateway.kodex-system.svc:8084"

// setRuntimeTransportFixture installs the exact synthetic transport allowlist through t.Setenv,
// restoring prior process values when the test completes without contacting the named gateway.
// setRuntimeTransportFixture устанавливает точный синтетический список транспорта через t.Setenv
// с восстановлением прежних значений после теста и без обращения к указанному gateway.
func setRuntimeTransportFixture(t *testing.T) {
	t.Helper()
	for _, name := range runtimeTransportEnvironmentNames {
		value := runtimeTrustBundle
		switch name {
		case "HTTP_PROXY", "HTTPS_PROXY":
			value = syntheticRuntimeProxy
		case "NO_PROXY":
			value = "127.0.0.1,localhost"
		}
		t.Setenv(name, value)
	}
}

// TestRuntimeTransportEnvironmentRejectsMissingOrForeignAuthority mutates required proxy, bypass and
// trust settings one at a time and requires rejection before transport can use foreign configuration.
// TestRuntimeTransportEnvironmentRejectsMissingOrForeignAuthority по одному подменяет обязательные proxy,
// bypass и CA, требуя отказа до использования транспортом чужой конфигурации.
func TestRuntimeTransportEnvironmentRejectsMissingOrForeignAuthority(t *testing.T) {
	for _, mutation := range []struct{ name, value string }{
		{"HTTPS_PROXY", ""}, {"HTTP_PROXY", "http://other.example"},
		{"HTTPS_PROXY", "http://kodex:fixture.signature@other.example:8084"},
		{"HTTPS_PROXY", syntheticRuntimeProxy + "/proxy"},
		{"NO_PROXY", "*"}, {"SSL_CERT_FILE", "/tmp/foreign-ca.pem"},
		{"NODE_EXTRA_CA_CERTS", "/tmp/foreign-ca.pem"},
	} {
		t.Run(mutation.name+mutation.value, func(t *testing.T) {
			setRuntimeTransportFixture(t)
			t.Setenv(mutation.name, mutation.value)
			if _, err := runtimeTransportEnvironment(); err == nil {
				t.Fatal("foreign or missing runtime transport authority was accepted")
			}
		})
	}
}
