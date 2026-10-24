package app

import "testing"

// TestEmailWorkerGrantTrustRegistration checks disabled email trust is absent and enabling it
// preserves exact integration/interaction mappings and the complete eleven-entry registry.
// TestEmailWorkerGrantTrustRegistration проверяет отсутствие отключённого email trust и сохранение
// точных integration и interaction mappings и полного реестра из одиннадцати записей при его
// включении.
func TestEmailWorkerGrantTrustRegistration(t *testing.T) {
	config := Config{IntegrationGrantTrustFile: "/trust/integration.jwk", InteractionGrantTrustFile: "/trust/interaction.jwk"}
	if _, ok := workerGrantTrustFilesFor(config)["email-bridge"]; ok {
		t.Fatal("disabled email trust was registered")
	}
	config.EmailGrantTrustFile = "/trust/email.jwk"
	files := workerGrantTrustFilesFor(config)
	if files["email-bridge"] != config.EmailGrantTrustFile || files["interaction-gateway"] != config.InteractionGrantTrustFile || files["integration-gateway"] != config.IntegrationGrantTrustFile || len(files) != 11 {
		t.Fatal("email trust registration changed another workload boundary")
	}
}
