package app

import "testing"

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
