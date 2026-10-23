package serviceidentity

import (
	"os"
	"testing"
)

// TestGeneratedControlPlanePolicyLoadsInActualAuthorizer checks that the checked-in generated policy is accepted in full and preserves an exact privileged binding.
// TestGeneratedControlPlanePolicyLoadsInActualAuthorizer проверяет, что сохранённая сгенерированная политика целиком принимается и сохраняет точную привилегированную привязку.
func TestGeneratedControlPlanePolicyLoadsInActualAuthorizer(t *testing.T) {
	raw, err := os.ReadFile("../../../../services/internal/control-plane/internal/app/service-identity-policy.json")
	if err != nil {
		t.Fatal(err)
	}
	authorizer, digest, err := FromPolicy(raw, target, &revocationCheck{})
	if err != nil || len(digest) != 64 || len(authorizer.bindings) != 417 {
		t.Fatal("generated policy rejected")
	}
	const caller = "spiffe://kodex.local/ns/kodex-system/sa/runtime-controller"
	const method = "/controlplane.v1.RuntimeWorkService/GetExecutionWorkflowCatalog"
	const operation = "platform.runtime.execution.workflow.catalog"
	binding, found := authorizer.bindings[caller+"\x00"+method]
	if !found || binding != (Binding{
		CallerSPIFFEID: caller, FullMethod: method, OperationID: operation,
		Permission: operation, ActorMode: ServiceActor, ProjectRequired: false,
	}) {
		t.Fatal("workflow catalog service binding rejected")
	}
	for _, binding := range authorizer.bindings {
		if binding.OperationID == "platform.stt.policy.resolve" {
			t.Fatal("delegation downgraded")
		}
	}
}

// TestPolicyRejectsDuplicateAndUnknownFields ensures malformed policy documents cannot gain authority through ambiguous or unrecognized JSON members.
// TestPolicyRejectsDuplicateAndUnknownFields проверяет, что некорректный документ политики не получает полномочия через неоднозначные или неизвестные поля JSON.
func TestPolicyRejectsDuplicateAndUnknownFields(t *testing.T) {
	for _, raw := range []string{
		`{"version":1,"version":1,"target_spiffe_id":"` + target + `","bindings":[]}`,
		`{"version":1,"target_spiffe_id":"` + target + `","bindings":[],"allow_all":true}`,
	} {
		if _, _, err := FromPolicy([]byte(raw), target, &revocationCheck{}); err == nil {
			t.Fatal("malformed policy accepted")
		}
	}
}
