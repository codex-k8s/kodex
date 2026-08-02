package controlplaneclient

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRuntimeAndIntegrationProfilesMatchAuthorityPolicy(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "deploy", "k8s", "base",
		"internal-rpc-authority-publisher", "authority-policy.json",
	))
	if err != nil {
		t.Fatalf("read authority policy: %v", err)
	}
	var document struct {
		Policy struct {
			Bindings []struct {
				ProducerID  string `json:"authority_proof_producer_id"`
				OperationID string `json:"operation_id"`
				FullMethod  string `json:"full_method"`
			} `json:"operation_bindings"`
		} `json:"policy"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("parse authority policy: %v", err)
	}
	tests := []struct {
		producerID string
		profile    map[string]string
	}{
		{producerID: "control-plane.runtime-controller", profile: RuntimeControllerOperations()},
		{producerID: "control-plane.integration-gateway", profile: IntegrationGatewayOperations()},
		{producerID: "control-plane.agent-session", profile: AgentRunnerOperations()},
	}
	for _, test := range tests {
		actual := make(map[string]string)
		for _, binding := range document.Policy.Bindings {
			if binding.ProducerID == test.producerID {
				actual[binding.OperationID] = binding.FullMethod
			}
		}
		if !reflect.DeepEqual(actual, test.profile) {
			t.Fatalf("operation profile mismatch for %s", test.producerID)
		}
	}
}
