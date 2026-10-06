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
					RecipientIntegrationGrants: &controlplanev1.AssistantRecipientIntegrationGrantCatalog{RecipientKind: kind, RecipientRef: input.AssistantContext.EntityRef, RecipientName: "Developer", RecipientVersion: version, ProjectVersion: 4,
						Entries: []*controlplanev1.AssistantRecipientIntegrationGrantCatalogEntry{{Grant: grant, Pins: &controlplanev1.IntegrationGrantCandidatePins{ConnectionVersion: 2, DefinitionVersion: "1.0.0", DefinitionDigest: strings.Repeat("a", 64), ContextDigest: strings.Repeat("b", 64), ProjectVersion: 4, RecipientVersion: version}}}}}
				if _, err := castAssistantConfigurationCatalog(input, request, response); err != nil {
					t.Fatal(err)
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
