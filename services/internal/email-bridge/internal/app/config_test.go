package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDatabaseTransport(t *testing.T) {
	const valid = "postgresql://email_bridge_runtime@" + "email-bridge-postgresql.kodex-system.svc.cluster.local:5432/email_bridge?sslmode=verify-full&sslrootcert=/var/run/email/tls/ca.crt"
	for _, suffix := range []string{"", "&host=foreign.example.test", "&sslmode=disable", "&user=postgres", "&service=override", "&sslrootcert=/other", "&hostaddr=127.0.0.1"} {
		path := filepath.Join(t.TempDir(), "dsn")
		if err := os.WriteFile(path, []byte(valid+suffix), 0440); err != nil {
			t.Fatal(err)
		}
		_, err := databaseDSN(path)
		if (err == nil) != (suffix == "") {
			t.Fatal("database transport validation mismatch")
		}
	}
}

func TestMailEgressDestination(t *testing.T) {
	t.Setenv("EMAIL_BRIDGE_CONFIGURATION_MODE", configurationBootstrap)
	t.Setenv("EMAIL_BRIDGE_EXPECTED_CONFIGURATION_REVISION", "0")
	t.Setenv("EMAIL_BRIDGE_EXPECTED_CONFIGURATION_DIGEST", "")
	t.Setenv("EMAIL_BRIDGE_EGRESS_POLICY_DIGEST", strings.Repeat("a", 64))
	for _, key := range []string{"EMAIL_BRIDGE_SECRETS_ROOT", "EMAIL_BRIDGE_DSN_FILE", "EMAIL_BRIDGE_CERTIFICATE_FILE", "EMAIL_BRIDGE_PRIVATE_KEY_FILE", "EMAIL_BRIDGE_CA_FILE", "EMAIL_BRIDGE_APPLICATION_GRANT_FILE", "OTEL_EXPORTER_OTLP_CA_FILE"} {
		t.Setenv(key, "/fixture/"+key)
	}
	t.Setenv("DEPLOYMENT_ENVIRONMENT", "test")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "otel.example.test:4317")
	t.Setenv("OTEL_EXPORTER_OTLP_TLS_SERVER_NAME", "otel.example.test")
	t.Setenv("EMAIL_BRIDGE_AUTHORITY_TARGET", "control-plane.kodex-system.svc.cluster.local:8443")
	t.Setenv("EMAIL_BRIDGE_RECONCILIATION_INTERVAL_SECONDS", "15")
	t.Setenv("EMAIL_BRIDGE_RECONCILIATION_BATCH", "16")
	for _, address := range []string{"egress-gateway.kodex-system.svc:8082", "egress-gateway.kodex-system.svc:8080", "egress-gateway.kodex-system.svc:8081", "foreign.example.test:8082", "127.0.0.1:8082"} {
		t.Run(address, func(t *testing.T) {
			t.Setenv("EMAIL_BRIDGE_EGRESS_ADDRESS", address)
			config, err := loadConfig()
			allowed := address == "egress-gateway.kodex-system.svc:8082"
			if (err == nil) != allowed || (allowed && config.EgressAddress != address) {
				t.Fatal("mail egress destination validation mismatch")
			}
		})
	}
	for _, digest := range []string{"", "invalid", strings.Repeat("A", 64), strings.Repeat("a", 63)} {
		t.Setenv("EMAIL_BRIDGE_EGRESS_ADDRESS", mailEgressAddress)
		t.Setenv("EMAIL_BRIDGE_EGRESS_POLICY_DIGEST", digest)
		if _, err := loadConfig(); err == nil {
			t.Fatal("invalid egress digest accepted")
		}
	}
	t.Setenv("EMAIL_BRIDGE_EGRESS_POLICY_DIGEST", strings.Repeat("a", 64))
	for _, item := range []struct {
		mode, revision, digest string
		valid                  bool
	}{
		{configurationBootstrap, "0", "", true},
		{configurationManaged, "7", strings.Repeat("b", 64), true},
		{"", "0", "", false},
		{"other", "0", "", false},
		{configurationManaged, "0", strings.Repeat("b", 64), false},
		{configurationManaged, "7", "", false},
		{configurationManaged, "-1", strings.Repeat("b", 64), false},
		{configurationManaged, "9223372036854775808", strings.Repeat("b", 64), false},
		{configurationBootstrap, "7", strings.Repeat("b", 64), false},
	} {
		t.Setenv("EMAIL_BRIDGE_CONFIGURATION_MODE", item.mode)
		t.Setenv("EMAIL_BRIDGE_EXPECTED_CONFIGURATION_REVISION", item.revision)
		t.Setenv("EMAIL_BRIDGE_EXPECTED_CONFIGURATION_DIGEST", item.digest)
		if _, err := loadConfig(); (err == nil) != item.valid {
			t.Fatal("configuration deployment environment validation mismatch")
		}
	}
}
