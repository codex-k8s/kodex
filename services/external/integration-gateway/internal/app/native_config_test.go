package app

import "testing"

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
