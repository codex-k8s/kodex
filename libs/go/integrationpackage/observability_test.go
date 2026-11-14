package integrationpackage

import "testing"

// TestObservabilityEndpointScopes requires explicit whole-endpoint scope in owner configuration and every capability, keeping tenant routing from masquerading as isolated authority.
// TestObservabilityEndpointScopes требует явную whole-endpoint scope в owner configuration и каждой capability, исключая подмену isolated authority tenant routing.
func TestObservabilityEndpointScopes(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"prometheus", "thanos"} {
		definition := definitions[key]
		configuration := map[string]string{"base_url": "https://metrics.example.invalid", "scope_mode": "WHOLE_ENDPOINT"}
		if key == "thanos" {
			configuration["tenant_id"] = "fixture-tenant"
		}
		if definition.ValidateConfiguration(configuration) != nil || len(definition.Spec.Capabilities) != 8 {
			t.Fatal("endpoint definition unavailable")
		}
		for _, capability := range definition.Spec.Capabilities {
			scope, err := capability.ResourceScopeValues(configuration)
			if err != nil || scope["scope_mode"] != "WHOLE_ENDPOINT" || len(scope) != len(configuration) || CredentialResourcePermissionProfile(definition, capability) != CredentialResourcePermissionObservabilityReadV1 || capability.Risk != "READ" || capability.ApprovalPolicy != "NONE" {
				t.Fatal("endpoint authority or typed profile mismatch")
			}
		}
		configuration["scope_mode"] = "TENANT_ISOLATED"
		if definition.ValidateConfiguration(configuration) == nil {
			t.Fatal("tenant header became isolation authority")
		}
		if _, exists := CredentialResourcePermissionContractFor(key, key+".config.update"); exists {
			t.Fatal("unsupported server config write received a contract")
		}
	}
}

// TestProviderJSONPreservesMetricCase distinguishes legitimate case-sensitive data from repeated-key ambiguity and case-fold permission aliases without allowing trailing documents or excessive depth.
// TestProviderJSONPreservesMetricCase различает регистрозависимые data и неоднозначность повторов/case-fold permission aliases, не допуская лишние документы либо глубину.
func TestProviderJSONPreservesMetricCase(t *testing.T) {
	if ValidateProviderJSON([]byte(`{"metric":{"a":"one","A":"two"}}`)) != nil || ValidateCredentialPermissionJSON([]byte(`{"READ":true,"read":false}`)) == nil {
		t.Fatal("data/permission case boundaries collapsed")
	}
	for _, value := range []string{`{"a":1,"a":2}`, `{} {}`, `{"a":`} {
		if ValidateProviderJSON([]byte(value)) == nil {
			t.Fatal("ambiguous provider JSON accepted")
		}
	}
}
