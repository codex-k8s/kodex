package callback

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/protobuf/proto"
)

func TestAssistantRecipientIntegrationCatalogClosedRead(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeProject, runtimecontract.AssistantScopeSystem} {
		for _, kind := range []string{"AGENT", "WORKFLOW"} {
			t.Run(string(scope)+kind, func(t *testing.T) {
				input, arguments, _ := assistantFreshCatalogFixture(scope, "RECIPIENT_INTEGRATION_GRANTS")
				version := int64(3)
				input.AssistantContext = &runtimecontract.RunnerAssistantContext{EntityKind: kind, EntityRef: "agt_recipient123", EntityVersion: &version, AllowedOperations: []string{"CHANGE_INTEGRATION_GRANT"}}
				catalogKinds := assistantConfigurationCatalogInputSchema(input)["properties"].(map[string]any)["kind"].(map[string]any)["enum"].([]string)
				if !slices.Contains(catalogKinds, "RECIPIENT_INTEGRATION_GRANTS") {
					t.Fatal("eligible dynamic tool index omitted recipient catalog")
				}
				selector := arguments["assistant_configuration_catalog"].(map[string]any)
				request, err := parseAssistantConfigurationCatalog(input, arguments, selector)
				if err != nil {
					t.Fatal(err)
				}
				schema := `{"type":"object","additionalProperties":false,"properties":{}}`
				hash := sha256.Sum256([]byte(schema))
				grant := &controlplanev1.ProjectAssistantIntegrationGrantCatalogEntry{ConnectionRef: "int_connection123", ConnectionName: "Repository", ConnectionVersion: 2, DefinitionVersion: "1.0.0", DefinitionDigest: strings.Repeat("a", 64),
					Candidate: &controlplanev1.SystemAssistantIntegrationGrantCandidate{Grantable: true, Reason: controlplanev1.IntegrationCandidateReason_INTEGRATION_CANDIDATE_REASON_READY, CurrentGrantRef: "igr_disabled123", CurrentGrantVersion: 7, CurrentApprovalPolicy: controlplanev1.IntegrationApprovalPolicy_INTEGRATION_APPROVAL_POLICY_NONE,
						Capability: &controlplanev1.IntegrationCapability{Key: "github.repository.read", Name: "Read", TypedRisk: controlplanev1.IntegrationRisk_INTEGRATION_RISK_READ, ApprovalPolicy: controlplanev1.IntegrationApprovalPolicy_INTEGRATION_APPROVAL_POLICY_NONE, AllowedApprovalPolicies: []controlplanev1.IntegrationApprovalPolicy{controlplanev1.IntegrationApprovalPolicy_INTEGRATION_APPROVAL_POLICY_NONE}, InputSchema: schema, InputSchemaSha256: hex.EncodeToString(hash[:])}}}
				projectRef := input.ProjectRef
				if projectRef == "" {
					projectRef = "prj_selected123"
				}
				response := &controlplanev1.AssistantConfigurationCatalogResponse{Kind: request.GetKind(), AssistantRef: input.AgentRef, OrganizationRef: input.OrganizationRef, ScopeKind: "PROJECT", ProjectRef: projectRef,
					RecipientIntegrationGrants: &controlplanev1.AssistantRecipientIntegrationGrantCatalog{RecipientKind: kind, RecipientRef: input.AssistantContext.EntityRef, RecipientName: "Developer", RecipientVersion: version, ProjectVersion: 4, ContextEntityKind: kind, ContextEntityRef: input.AssistantContext.EntityRef, ContextEntityVersion: version,
						Entries: []*controlplanev1.AssistantRecipientIntegrationGrantCatalogEntry{{Grant: grant, Pins: &controlplanev1.IntegrationGrantCandidatePins{ConnectionVersion: 2, DefinitionVersion: "1.0.0", DefinitionDigest: strings.Repeat("a", 64), ContextDigest: strings.Repeat("b", 64), ProjectVersion: 4, RecipientVersion: version}}}}}
				if _, err := castAssistantConfigurationCatalog(input, request, response); err != nil {
					t.Fatal(err)
				}
				unavailable := proto.Clone(response).(*controlplanev1.AssistantConfigurationCatalogResponse)
				retired := unavailable.RecipientIntegrationGrants.Entries[0].Grant.Candidate
				retired.Reason, retired.Grantable, retired.CurrentGrantEnabled = controlplanev1.IntegrationCandidateReason_INTEGRATION_CANDIDATE_REASON_PACKAGE_UNAVAILABLE, false, true
				projection, err := castAssistantConfigurationCatalog(input, request, unavailable)
				if err != nil {
					t.Fatal("retired package metadata was not readable", err)
				}
				projected := projection["recipient_integration_grants"].(map[string]any)["entries"].([]map[string]any)[0]["candidate"].(map[string]any)
				if projected["grantable"] != false || projected["reason"] != "PACKAGE_UNAVAILABLE" || projected["current_grant_enabled"] != true {
					t.Fatal("retired metadata fabricated stored grant state or execution eligibility")
				}
				retired.Grantable = true
				if _, err := castAssistantConfigurationCatalog(input, request, unavailable); err == nil {
					t.Fatal("unavailable package gained grant authority")
				}
				if kind == "WORKFLOW" {
					selectedInput := input
					context := *input.AssistantContext
					context.AllowedOperations = []string{"CHANGE_INTEGRATION_GRANT", "UPDATE_WORKFLOW"}
					selectedInput.AssistantContext = &context
					selectedRequest := proto.Clone(request).(*controlplanev1.AssistantConfigurationCatalogRequest)
					selectedRequest.EntityKind, selectedRequest.EntityRef = "AGENT", "agt_assigned123"
					selected := proto.Clone(response).(*controlplanev1.AssistantConfigurationCatalogResponse)
					selected.RecipientIntegrationGrants.RecipientKind, selected.RecipientIntegrationGrants.RecipientRef = "AGENT", selectedRequest.EntityRef
					selected.RecipientIntegrationGrants.RecipientVersion = 19
					selected.RecipientIntegrationGrants.Entries[0].Pins.RecipientVersion = 19
					if _, err := castAssistantConfigurationCatalog(selectedInput, selectedRequest, selected); err != nil {
						t.Fatal("assigned AGENT projection rejected", err)
					}
					selected.RecipientIntegrationGrants.ContextEntityVersion++
					if _, err := castAssistantConfigurationCatalog(selectedInput, selectedRequest, selected); err == nil {
						t.Fatal("selected AGENT lost immutable Workflow context pin")
					}
				}
				for _, mutate := range []func(*controlplanev1.AssistantConfigurationCatalogResponse){
					func(r *controlplanev1.AssistantConfigurationCatalogResponse) {
						r.RecipientIntegrationGrants.RecipientRef = "agt_other12345"
					},
					func(r *controlplanev1.AssistantConfigurationCatalogResponse) {
						r.RecipientIntegrationGrants.RecipientKind = "PROJECT"
					},
					func(r *controlplanev1.AssistantConfigurationCatalogResponse) {
						r.RecipientIntegrationGrants.RecipientVersion++
					},
					func(r *controlplanev1.AssistantConfigurationCatalogResponse) { r.OrganizationRef = "org_foreign123" },
					func(r *controlplanev1.AssistantConfigurationCatalogResponse) {
						r.AssistantProfileRef = "asstp_false123"
					},
					func(r *controlplanev1.AssistantConfigurationCatalogResponse) {
						r.RecipientIntegrationGrants.Entries[0].Pins.ConnectionVersion++
					},
					func(r *controlplanev1.AssistantConfigurationCatalogResponse) {
						r.RecipientIntegrationGrants.Entries[0].Grant.Candidate.Capability.InputSchemaSha256 = strings.Repeat("d", 64)
					},
					func(r *controlplanev1.AssistantConfigurationCatalogResponse) {
						r.RecipientIntegrationGrants.Entries[0].ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01})
					},
					func(r *controlplanev1.AssistantConfigurationCatalogResponse) {
						r.ProjectIntegrationGrants = []*controlplanev1.ProjectAssistantIntegrationGrantCatalogEntry{grant}
					},
					func(r *controlplanev1.AssistantConfigurationCatalogResponse) {
						r.RecipientIntegrationGrants.Entries = append(r.RecipientIntegrationGrants.Entries, r.RecipientIntegrationGrants.Entries[0])
					},
				} {
					bad := proto.Clone(response).(*controlplanev1.AssistantConfigurationCatalogResponse)
					mutate(bad)
					if _, err := castAssistantConfigurationCatalog(input, request, bad); err == nil {
						t.Fatal("accepted unbound recipient catalog")
					}
				}
				if scope == runtimecontract.AssistantScopeProject {
					response.ProjectRef = "prj_other12345"
					if _, err := castAssistantConfigurationCatalog(input, request, response); err == nil {
						t.Fatal("accepted foreign project")
					}
				}
				for _, mutation := range []func(*runtimecontract.RunnerInput){
					func(i *runtimecontract.RunnerInput) { i.AssistantContext.AllowedOperations = nil },
					func(i *runtimecontract.RunnerInput) { i.AssistantContext.EntityKind = "PROJECT" },
					func(i *runtimecontract.RunnerInput) { i.AssistantContext.EntityVersion = nil },
					func(i *runtimecontract.RunnerInput) { i.LeaseFence = "" },
				} {
					bad := input
					context := *input.AssistantContext
					bad.AssistantContext = &context
					mutation(&bad)
					if bad.AssistantContext.AllowedOperations == nil || bad.AssistantContext.EntityKind == "PROJECT" || bad.AssistantContext.EntityVersion == nil {
						kinds := assistantConfigurationCatalogInputSchema(bad)["properties"].(map[string]any)["kind"].(map[string]any)["enum"].([]string)
						if slices.Contains(kinds, "RECIPIENT_INTEGRATION_GRANTS") {
							t.Fatal("ineligible tool index exposed recipient catalog")
						}
					}
					if _, err := parseAssistantConfigurationCatalog(bad, arguments, selector); err == nil {
						t.Fatal("accepted unavailable context")
					}
				}
				selector["recipient_ref"] = "agt_other12345"
				if _, err := parseAssistantConfigurationCatalog(input, arguments, selector); err == nil {
					t.Fatal("accepted payload recipient authority")
				}
			})
		}
	}
}
