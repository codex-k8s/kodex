package codex

import (
	"os"
	"slices"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
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

// TestAppServerEnvironmentPreservesExactTrustWithoutCredentialInheritance verifies the child receives
// only fixed runtime settings and explicitly projected secrets, without unrelated inherited values;
// user values and secret projections cannot overwrite server-owned transport or disable TLS checks.
// TestAppServerEnvironmentPreservesExactTrustWithoutCredentialInheritance проверяет передачу дочернему
// процессу только заданного окружения и явно спроецированных секретов без посторонних переменных;
// пользовательские значения и secret projections не подменяют транспорт и не отключают TLS-проверки.
func TestAppServerEnvironmentPreservesExactTrustWithoutCredentialInheritance(t *testing.T) {
	setRuntimeTransportFixture(t)
	t.Setenv("UNRELATED_SECRET", "synthetic-unrelated")
	t.Setenv("SSL_CERT_DIR", "/tmp/foreign")
	t.Setenv("GIT_SSL_NO_VERIFY", "true")
	t.Setenv("SERVICE_TOKEN", "synthetic-bound-secret")
	input := model.Input{CodexHome: "/workspace/.kodex", SecretProjections: []runtimecontract.RuntimeSecretProjection{{Name: "SERVICE_TOKEN"}}}
	actual := appServerEnvironment(input, "synthetic-mcp-token")
	for _, name := range runtimeTransportEnvironmentNames {
		if !slices.Contains(actual, name+"="+os.Getenv(name)) {
			t.Fatalf("server-owned transport variable %s is missing", name)
		}
	}
	if !slices.Contains(actual, "SERVICE_TOKEN=synthetic-bound-secret") || len(actual) != 4+len(runtimeTransportEnvironmentNames)+1 {
		t.Fatal("child environment is not the exact approved set")
	}
	for _, name := range []string{"CURL_CA_BUNDLE", "NODE_EXTRA_CA_CERTS", "GIT_SSL_NO_VERIFY", "HTTP_PROXY"} {
		input.EnvironmentValues = []runtimecontract.RuntimeEnvironmentValue{{Name: name, Value: "override"}}
		if appServerEnvironment(input, "synthetic-mcp-token") != nil {
			t.Fatal("user environment overrode server-owned transport")
		}
		input.EnvironmentValues = nil
		input.SecretProjections = []runtimecontract.RuntimeSecretProjection{{Name: name}}
		if appServerEnvironment(input, "synthetic-mcp-token") != nil {
			t.Fatal("Secret projection overrode server-owned transport")
		}
	}
}
