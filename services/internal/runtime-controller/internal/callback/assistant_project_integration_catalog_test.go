package callback

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/protobuf/proto"
)

func TestProjectAssistantIntegrationGrantNativeCatalog(t *testing.T) {
	input, args, response := assistantFreshCatalogFixture(runtimecontract.AssistantScopeProject, "PROJECT_INTEGRATION_GRANTS")
	schema := `{"type":"object","additionalProperties":false,"properties":{}}`
	digest := sha256.Sum256([]byte(schema))
	catalog := response.AssistantConfigurationCatalog
	catalog.Entries = nil
	catalog.ProjectIntegrationGrants = []*controlplanev1.ProjectAssistantIntegrationGrantCatalogEntry{{
		ConnectionRef: "int_owned12345", ConnectionName: "Own repository", ConnectionVersion: 4, DefinitionVersion: "2.3.1", DefinitionDigest: strings.Repeat("a", 64),
		Candidate: &controlplanev1.SystemAssistantIntegrationGrantCandidate{Grantable: true, Reason: controlplanev1.IntegrationCandidateReason_INTEGRATION_CANDIDATE_REASON_READY,
			Capability: &controlplanev1.IntegrationCapability{Key: "github.repository.read", Name: "Repository", TypedRisk: controlplanev1.IntegrationRisk_INTEGRATION_RISK_READ,
				ApprovalPolicy: controlplanev1.IntegrationApprovalPolicy_INTEGRATION_APPROVAL_POLICY_NONE, AllowedApprovalPolicies: []controlplanev1.IntegrationApprovalPolicy{controlplanev1.IntegrationApprovalPolicy_INTEGRATION_APPROVAL_POLICY_NONE}, InputSchema: schema, InputSchemaSha256: hex.EncodeToString(digest[:])}},
	}}
	selector := args["assistant_configuration_catalog"].(map[string]any)
	request, err := parseAssistantConfigurationCatalog(input, args, selector)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := castAssistantConfigurationCatalog(input, request, catalog)
	if err != nil {
		t.Fatal(err)
	}
	rows := projection["project_integration_grants"].([]map[string]any)
	if len(rows) != 1 || rows[0]["connection_ref"] != "int_owned12345" || projection["project_ref"] != input.ProjectRef {
		t.Fatal("native grant catalog lost exact owner")
	}
	for _, mutate := range []func(*controlplanev1.AssistantConfigurationCatalogResponse){
		func(c *controlplanev1.AssistantConfigurationCatalogResponse) { c.ProjectRef = "prj_foreign123" },
		func(c *controlplanev1.AssistantConfigurationCatalogResponse) {
			c.AssistantProfileRef = "asstp_foreign123"
		},
		func(c *controlplanev1.AssistantConfigurationCatalogResponse) {
			c.Entries = []*controlplanev1.AssistantConfigurationCatalogEntry{{Ref: "unexpected"}}
		}, // replaced below
		func(c *controlplanev1.AssistantConfigurationCatalogResponse) {
			c.ProjectIntegrationGrants[0].Candidate.Reason = 999
		},
		func(c *controlplanev1.AssistantConfigurationCatalogResponse) {
			c.ProjectIntegrationGrants[0].Candidate.Capability.InputSchemaSha256 = strings.Repeat("b", 64)
		},
		func(c *controlplanev1.AssistantConfigurationCatalogResponse) {
			c.ProjectIntegrationGrants = append(c.ProjectIntegrationGrants, c.ProjectIntegrationGrants[0])
		},
	} {
		bad := proto.Clone(catalog).(*controlplanev1.AssistantConfigurationCatalogResponse)
		mutate(bad)
		if _, err := castAssistantConfigurationCatalog(input, request, bad); err == nil {
			t.Fatal("accepted unbound/malformed catalog")
		}
	}
	input.AssistantScope = runtimecontract.AssistantScopeSystem
	if _, err := parseAssistantConfigurationCatalog(input, args, selector); err == nil {
		t.Fatal("SYSTEM accepted PROJECT grant catalog")
	}
}
func TestProjectAssistantIntegrationGrantToolClosedParameters(t *testing.T) {
	input := assistantConfigurationFixture(runtimecontract.AssistantScopeProject)
	input.AssistantContext.AllowedOperations = append(input.AssistantContext.AllowedOperations, "CHANGE_PROJECT_ASSISTANT_INTEGRATION_GRANT")
	good := map[string]any{"projectAssistantRef": input.AgentRef, "connectionRef": "int_owned12345", "capabilityKey": "github.repository.read", "enabled": true, "approvalPolicy": "NONE"}
	if !assistantConfigurationParametersAllowed(input, "CHANGE_PROJECT_ASSISTANT_INTEGRATION_GRANT", good) {
		t.Fatal("exact PROJECT params rejected")
	}
	for _, field := range []string{"organizationRef", "agentRef", "assistantProfileRef", "actorRef", "scopeKind"} {
		bad := map[string]any{}
		for key, value := range good {
			bad[key] = value
		}
		bad[field] = "caller"
		if assistantConfigurationParametersAllowed(input, "CHANGE_PROJECT_ASSISTANT_INTEGRATION_GRANT", bad) {
			t.Fatal("caller server pin accepted")
		}
	}
	good["projectAssistantRef"] = "agt_foreign123"
	if assistantConfigurationParametersAllowed(input, "CHANGE_PROJECT_ASSISTANT_INTEGRATION_GRANT", good) {
		t.Fatal("foreign helper accepted")
	}
	good["projectAssistantRef"] = input.AgentRef
	input.AssistantScope = runtimecontract.AssistantScopeSystem
	if assistantConfigurationParametersAllowed(input, "CHANGE_PROJECT_ASSISTANT_INTEGRATION_GRANT", good) {
		t.Fatal("SYSTEM accepted PROJECT self grant")
	}
}
