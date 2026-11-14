package app

import (
	"strings"
	"testing"
)

// TestNativeProxyConfigurationIsOptionalAndExact checks process parsing independently of
// adapter construction, rejecting URL credentials, fragments and alternate in-cluster peers.
// TestNativeProxyConfigurationIsOptionalAndExact проверяет process parsing отдельно от
// adapter construction, отклоняя URL credentials, fragments и другие peers внутри кластера.
func TestNativeProxyConfigurationIsOptionalAndExact(t *testing.T) {
	t.Setenv("DEPLOYMENT_ENVIRONMENT", "staging")
	for _, value := range []string{"", "http://egress-gateway-native.kodex-system.svc.cluster.local:8085"} {
		t.Setenv("INTEGRATION_GATEWAY_NATIVE_PROXY_URL", value)
		config, err := loadConfig()
		if err != nil || config.NativeProxyURL != value {
			t.Fatal("supported optional native config rejected", err)
		}
	}
	for _, value := range []string{"http://other.example.invalid:8085", "http://egress-gateway-native.kodex-system.svc.cluster.local:8085/#fragment", "https://egress-gateway-native.kodex-system.svc.cluster.local:8085"} {
		t.Setenv("INTEGRATION_GATEWAY_NATIVE_PROXY_URL", value)
		if _, err := loadConfig(); err == nil {
			t.Fatal("substituted native process peer accepted")
		}
	}
}

// TestObservabilityQualificationProcessConfiguration rejects partial pins, alternate files and missing native transport during typed process parsing, before dependencies are opened.
// TestObservabilityQualificationProcessConfiguration отклоняет partial pins, alternate files и missing native transport при typed process parsing до открытия dependencies.
func TestObservabilityQualificationProcessConfiguration(t *testing.T) {
	t.Setenv("DEPLOYMENT_ENVIRONMENT", "staging")
	for _, scenario := range []string{"disabled", "enabled", "missing-digest", "missing-file", "alternate-file", "zero-digest", "missing-native"} {
		t.Run(scenario, func(t *testing.T) {
			file, digest, native := "/var/run/kodex/observability-qualifications/profiles.json", strings.Repeat("1", 64), "http://egress-gateway-native.kodex-system.svc.cluster.local:8085"
			switch scenario {
			case "disabled":
				file, digest, native = "", "", ""
			case "missing-digest":
				digest = ""
			case "missing-file":
				file = ""
			case "alternate-file":
				file = "/caller/profiles.json"
			case "zero-digest":
				digest = strings.Repeat("0", 64)
			case "missing-native":
				native = ""
			}
			t.Setenv("INTEGRATION_GATEWAY_OBSERVABILITY_QUALIFICATION_FILE", file)
			t.Setenv("INTEGRATION_GATEWAY_OBSERVABILITY_QUALIFICATION_SHA256", digest)
			t.Setenv("INTEGRATION_GATEWAY_NATIVE_PROXY_URL", native)
			_, err := loadConfig()
			if (err == nil) != (scenario == "disabled" || scenario == "enabled") {
				t.Fatal("operator process inputs crossed typed configuration boundary", err)
			}
		})
	}
}
