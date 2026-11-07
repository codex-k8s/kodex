package app

import (
	"strings"
	"testing"
)

// nativeConfigurationFixture supplies syntactically valid defaults without reading files or secrets.
// nativeConfigurationFixture задаёт syntactically valid defaults без чтения файлов либо секретов.
func nativeConfigurationFixture() Config {
	return Config{PolicyFile: "/policy.json", ResolverConfig: "/resolv.conf", ExpectedRevision: "synthetic", ExpectedDigest: strings.Repeat("a", 64), ConnectAddress: ":8080", TechnicalAddress: ":9090", STTConnectAddress: ":8081", MailConnectAddress: ":8082", IntegrationConnectAddress: ":8083", RuntimeConnectAddress: ":8084", MailPolicyFile: "/mail.json", IntegrationPolicyFile: "/integration.json", MailExpectedDigest: strings.Repeat("b", 64), IntegrationExpectedDigest: strings.Repeat("c", 64), RuntimeSigningKeyFile: "/signing", RuntimeProxyCACertificate: "/ca.pem", RuntimeProxyCAPrivateKey: "/ca.key"}
}

// TestNativeListenerTupleIsOptionalAndBounded keeps default deployment unchanged and
// requires an explicit complete443-route projection on the fixed optional8085 listener.
// TestNativeListenerTupleIsOptionalAndBounded сохраняет default deployment и требует
// полный явный набор443-маршрутов на фиксированном optional8085 listener.
func TestNativeListenerTupleIsOptionalAndBounded(t *testing.T) {
	base := nativeConfigurationFixture()
	if err := base.validate(); err != nil {
		t.Fatal(err)
	}
	valid := base
	valid.NativeConnectAddress = ":8085"
	valid.NativePolicyFile = "/native/policy.json"
	valid.NativeExpectedDigest = strings.Repeat("d", 64)
	if err := valid.validate(); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"missing-address", "missing-path", "missing-digest", "relative-path", "wrong-port", "bad-digest", "clamav"} {
		changed := valid
		switch scenario {
		case "missing-address":
			changed.NativeConnectAddress = ""
		case "missing-path":
			changed.NativePolicyFile = ""
		case "missing-digest":
			changed.NativeExpectedDigest = ""
		case "relative-path":
			changed.NativePolicyFile = "native.json"
		case "wrong-port":
			changed.NativeConnectAddress = ":8080"
		case "bad-digest":
			changed.NativeExpectedDigest = strings.Repeat("D", 64)
		case "clamav":
			changed.Mode = "clamav"
		}
		if changed.validate() == nil {
			t.Fatal("invalid optional native tuple accepted", scenario)
		}
	}
}
