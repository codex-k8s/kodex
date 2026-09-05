package httptransport

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

func TestIntegrationPublicEnumsAndRiskPreserveAuthorityMeaning(t *testing.T) {
	for risk, riskName := range map[cp.IntegrationRisk]string{cp.IntegrationRisk_INTEGRATION_RISK_READ: "READ", cp.IntegrationRisk_INTEGRATION_RISK_WRITE: "WRITE", cp.IntegrationRisk_INTEGRATION_RISK_SENSITIVE: "SENSITIVE", cp.IntegrationRisk_INTEGRATION_RISK_DESTRUCTIVE: "DESTRUCTIVE"} {
		for policy, policyName := range map[cp.IntegrationApprovalPolicy]string{cp.IntegrationApprovalPolicy_INTEGRATION_APPROVAL_POLICY_NONE: "NONE", cp.IntegrationApprovalPolicy_INTEGRATION_APPROVAL_POLICY_HUMAN_EACH_EFFECT: "HUMAN_EACH_EFFECT"} {
			for kind, kindName := range cp.IntegrationResourceKind_name {
				if kind == 0 {
					continue
				}
				value, err := messageMap(&cp.IntegrationGrant{Ref: "igr_fixture01", AgentRef: "agt_fixture01", TypedRisk: risk, ApprovalPolicy: policy, ResourceScope: &cp.IntegrationResourceScope{Kind: cp.IntegrationResourceKind(kind)}})
				if err != nil || value["risk"] != riskName || value["approvalPolicy"] != policyName || value["agentRef"] != "agt_fixture01" || value["enabled"] != false {
					t.Fatal("grant risk, approval or recipient changed")
				}
				if _, exists := value["typedRisk"]; exists {
					t.Fatal("internal risk duplicate leaked")
				}
				if value["resourceScope"].(map[string]any)["kind"] != strings.TrimPrefix(kindName, "INTEGRATION_RESOURCE_KIND_") {
					t.Fatal("internal resource kind leaked")
				}
			}
		}
	}
	value, err := messageMap(&cp.IntegrationDefinition{Origin: cp.IntegrationDefinitionOrigin_INTEGRATION_DEFINITION_ORIGIN_SHIPPED})
	if err != nil || value["origin"] != "SHIPPED" || value["available"] != false || value["builtIn"] != false {
		t.Fatal("definition source/readiness changed")
	}
}

func TestIntegrationRejectsUnknownEnumsAndContradictoryRisk(t *testing.T) {
	for _, capability := range []*cp.IntegrationCapability{
		{TypedRisk: cp.IntegrationRisk(999)},
		{TypedRisk: cp.IntegrationRisk_INTEGRATION_RISK_WRITE, Risk: "READ"},
		{Risk: "FUTURE_RISK"},
		{ApprovalPolicy: cp.IntegrationApprovalPolicy(999)},
		{ResourceKind: cp.IntegrationResourceKind(999)},
	} {
		w := httptest.NewRecorder()
		writeMessage(w, 200, &cp.ListIntegrationDefinitionsResponse{Definitions: []*cp.IntegrationDefinition{{Capabilities: []*cp.IntegrationCapability{capability}}}}, "", "definitions")
		if w.Code != 502 {
			t.Fatalf("invalid integration contract accepted: %d", w.Code)
		}
	}
	w := httptest.NewRecorder()
	writeMessage(w, 200, &cp.IntegrationDefinition{Origin: cp.IntegrationDefinitionOrigin(999)}, "", "")
	if w.Code != 502 {
		t.Fatal("unknown definition origin accepted")
	}
}

func TestIntegrationOptionalZeroBoundsRemainExplicit(t *testing.T) {
	for _, present := range []bool{false, true} {
		value, err := messageMap(&cp.IntegrationConfigurationField{Key: "limit", ValueType: "INTEGER", HasMinimum: present, HasMaximum: present})
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"minimum", "maximum"} {
			v, exists := value[key]
			if exists != present || exists && v != float64(0) {
				t.Fatal("optional zero bound lost")
			}
		}
		for _, key := range []string{"hasMinimum", "hasMaximum"} {
			if _, exists := value[key]; exists {
				t.Fatal("internal presence flag leaked")
			}
		}
		if value["required"] != false {
			t.Fatal("optional field became missing boolean")
		}
	}
	value, err := messageMap(&cp.IntegrationConfigurationField{HasMinimum: true, Minimum: -7, HasMaximum: true, Maximum: 21})
	if err != nil || value["minimum"] != float64(-7) || value["maximum"] != float64(21) {
		t.Fatal("numeric bounds changed")
	}
}

func TestIntegrationInputSchemaRequiresExactDigest(t *testing.T) {
	schema := `{"type":"object","properties":{"value":{"type":"string"}}}`
	sum := sha256.Sum256([]byte(schema))
	digest := hex.EncodeToString(sum[:])
	for _, invalid := range []string{"", "mismatch", "missing_schema", "missing_digest", "syntax", "oversize"} {
		capability := &cp.IntegrationCapability{InputSchema: schema, InputSchemaSha256: digest}
		switch invalid {
		case "mismatch":
			capability.InputSchemaSha256 = strings.Repeat("0", 64)
		case "missing_schema":
			capability.InputSchema = ""
		case "missing_digest":
			capability.InputSchemaSha256 = ""
		case "syntax":
			capability.InputSchema = "{"
		case "oversize":
			capability.InputSchema = `{"description":"` + strings.Repeat("x", 256<<10) + `"}`
		}
		w := httptest.NewRecorder()
		writeMessage(w, 200, &cp.ListIntegrationDefinitionsResponse{Definitions: []*cp.IntegrationDefinition{{Capabilities: []*cp.IntegrationCapability{capability}}}}, "", "definitions")
		if invalid != "" {
			if w.Code != 502 {
				t.Fatal("invalid schema digest returned")
			}
			continue
		}
		value, err := messageMap(capability)
		if w.Code != 200 || err != nil || value["inputSchema"] != schema || value["inputSchemaSha256"] != digest {
			t.Fatal("canonical schema changed")
		}
	}
}
