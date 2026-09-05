package mattermost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/credentialfs"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/mattermost/mattermost/server/public/model"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

func claimFixture(t *testing.T, key string, input map[string]any) (*Adapter, *controlplanev1.IntegrationInvocationClaim) {
	t.Helper()
	adapter, err := New(Config{CredentialDirectory: t.TempDir(), ProxyURL: "http://" + egressProxyHost, AllowedHosts: "chat.example.test", Timeout: time.Second}, localizerForTest(t))
	if err != nil {
		t.Fatal(err)
	}
	configuration := map[string]string{"base_url": "https://chat.example.test", "team_name": "team", "channel_name": "channel"}
	configurationProto, _ := structpb.NewStruct(map[string]any{"base_url": configuration["base_url"], "team_name": "team", "channel_name": "channel"})
	capability, ok := adapter.definition.Capability(key)
	if !ok {
		t.Fatal("fixture capability missing")
	}
	scope, err := capability.ResourceScopeValues(configuration)
	if err != nil {
		t.Fatal(err)
	}
	encodedScope, _ := json.Marshal(scope)
	scopeDigest := sha256.Sum256(encodedScope)
	encoded, _ := json.Marshal(input)
	canonical, err := capability.ValidateInput(encoded)
	if err != nil {
		t.Fatal(err)
	}
	inputDigest := sha256.Sum256(canonical)
	inputProto, err := structpb.NewStruct(input)
	if err != nil {
		t.Fatal(err)
	}
	definitionPackage, err := json.Marshal(adapter.definition)
	if err != nil {
		t.Fatal(err)
	}
	return adapter, &controlplanev1.IntegrationInvocationClaim{
		DefinitionPackage: definitionPackage,
		InvocationRef:     "inv_fixture", DefinitionKey: "mattermost", ConnectionRef: "connection",
		DefinitionVersion: adapter.definition.Metadata.Version, DefinitionDigest: adapter.definition.Digest,
		CapabilityKey: key, Operation: capability.Operation, EffectKey: "eff_fixture", InputDigest: hex.EncodeToString(inputDigest[:]),
		Risk:                controlplanev1.IntegrationRisk(controlplanev1.IntegrationRisk_value["INTEGRATION_RISK_"+capability.Risk]),
		ApprovalPolicy:      controlplanev1.IntegrationApprovalPolicy(controlplanev1.IntegrationApprovalPolicy_value["INTEGRATION_APPROVAL_POLICY_"+capability.ApprovalPolicy]),
		PublicConfiguration: configurationProto, BoundedInput: inputProto,
		ResourceScope: &controlplanev1.IntegrationResourceScope{Kind: controlplanev1.IntegrationResourceKind_INTEGRATION_RESOURCE_KIND_MATTERMOST_CHANNEL, Values: scope, Digest: hex.EncodeToString(scopeDigest[:])},
	}
}

func TestInvocationRejectsChangedContractScopeAndInput(t *testing.T) {
	adapter, original := claimFixture(t, "mattermost.post.send", map[string]any{"message": "new message"})
	if _, _, _, err := adapter.validateInvocation(original); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*controlplanev1.IntegrationInvocationClaim){
		"definition": func(claim *controlplanev1.IntegrationInvocationClaim) { claim.DefinitionKey = "github" },
		"version":    func(claim *controlplanev1.IntegrationInvocationClaim) { claim.DefinitionVersion = "1.0.0" },
		"digest": func(claim *controlplanev1.IntegrationInvocationClaim) {
			claim.DefinitionDigest = strings.Repeat("0", 64)
		},
		"operation": func(claim *controlplanev1.IntegrationInvocationClaim) { claim.Operation = "mattermost.reaction.add" },
		"risk": func(claim *controlplanev1.IntegrationInvocationClaim) {
			claim.Risk = controlplanev1.IntegrationRisk_INTEGRATION_RISK_READ
		},
		"approval": func(claim *controlplanev1.IntegrationInvocationClaim) {
			claim.ApprovalPolicy = controlplanev1.IntegrationApprovalPolicy_INTEGRATION_APPROVAL_POLICY_NONE
		},
		"scope": func(claim *controlplanev1.IntegrationInvocationClaim) {
			claim.ResourceScope.Values["channel_name"] = "other"
		},
		"scope_digest": func(claim *controlplanev1.IntegrationInvocationClaim) {
			claim.ResourceScope.Digest = strings.Repeat("0", 64)
		},
		"input_digest": func(claim *controlplanev1.IntegrationInvocationClaim) { claim.InputDigest = strings.Repeat("0", 64) },
		"unknown_input": func(claim *controlplanev1.IntegrationInvocationClaim) {
			claim.BoundedInput.Fields["channel_id"] = structpb.NewStringValue("other")
		},
		"effect": func(claim *controlplanev1.IntegrationInvocationClaim) { claim.EffectKey = "" },
	} {
		t.Run(name, func(t *testing.T) {
			claim := proto.Clone(original).(*controlplanev1.IntegrationInvocationClaim)
			mutate(claim)
			if _, _, _, err := adapter.validateInvocation(claim); err == nil {
				t.Fatal("changed authority-bearing input accepted")
			}
		})
	}
}

func TestSystemSubscriptionsCannotBeInvokedByAgent(t *testing.T) {
	for _, key := range []string{"mattermost.inbound", "mattermost.gate_decisions"} {
		input := map[string]any{}
		if key == "mattermost.gate_decisions" {
			input = map[string]any{"decision_ref": "gate_fixture", "decision": "APPROVE"}
		}
		adapter, claim := claimFixture(t, key, input)
		if _, _, _, err := adapter.validateInvocation(claim); err == nil {
			t.Fatal("agent can invoke system subscription")
		}
	}
}

func TestInvocationReceiptAndUncertainMutation(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		wantUnknown bool
	}{
		{name: "success", status: 201}, {name: "forbidden", status: 403}, {name: "provider_failure", status: 503, wantUnknown: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter, claim := claimFixture(t, "mattermost.post.send", map[string]any{"message": "new message"})
			capability, _, input, err := adapter.validateInvocation(claim)
			if err != nil {
				t.Fatal(err)
			}
			client := operationFixture(t, func(request *http.Request) (*http.Response, bool) {
				if request.Method == http.MethodPost && tc.status != 201 {
					return testResponse(tc.status, `{"status_code":`+strconv.Itoa(tc.status)+`}`), true
				}
				return nil, false
			})
			receipt, err := executeClaim(t.Context(), client, testChannel(), claim, capability, input)
			success, _, unknown := InvocationOutcome(err)
			if success != (tc.status == 201) || unknown != tc.wantUnknown {
				t.Fatalf("success=%v unknown=%v err=%v", success, unknown, err)
			}
			if success {
				digest := sha256.Sum256([]byte(receipt.GetResultSummary()))
				if receipt.GetEffectKey() != claim.GetEffectKey() || receipt.GetInputDigest() != claim.GetInputDigest() || receipt.GetResponseDigest() != hex.EncodeToString(digest[:]) || receipt.GetProviderEffectRef() != testPostID {
					t.Fatal("receipt is not content/intent bound")
				}
			}
		})
	}
}

func TestCredentialProjectionRequiresExactContentAndBoundedWait(t *testing.T) {
	adapter, _ := claimFixture(t, "mattermost.team.read", map[string]any{})
	root := t.TempDir()
	var err error
	adapter.credentials, err = credentialfs.New(root)
	if err != nil {
		t.Fatal(err)
	}
	value := []byte("synthetic-token")
	digest := sha256.Sum256(value)
	if err := os.WriteFile(filepath.Join(root, "token-key"), value, 0400); err != nil {
		t.Fatal(err)
	}
	credential := &controlplanev1.IntegrationCredentialRevision{Ref: "credential", Revision: 1, SecretRef: credentialSecretPrefix + "token-key", SecretUid: "00000000-0000-4000-8000-000000000001", SecretResourceVersion: "7", ContentSha256: hex.EncodeToString(digest[:])}
	actual, err := adapter.readInvocationCredential(t.Context(), credential)
	if err != nil || string(actual) != string(value) {
		t.Fatalf("credential read = %v", err)
	}
	clear(actual)
	credential.ContentSha256 = strings.Repeat("0", 64)
	if _, err := adapter.readInvocationCredential(t.Context(), credential); err == nil {
		t.Fatal("different credential generation accepted")
	}
	credential.SecretRef = credentialSecretPrefix + "missing"
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Millisecond)
	defer cancel()
	if _, err := adapter.readInvocationCredential(ctx, credential); err == nil {
		t.Fatal("missing credential did not close")
	}
	adapter.timeout = 20 * time.Millisecond
	started := time.Now()
	if _, err := adapter.readInvocationCredential(t.Context(), credential); err == nil || time.Since(started) > time.Second {
		t.Fatal("credential wait exceeded adapter budget")
	}
	credential.SecretRef = credentialSecretPrefix + "token-key"
	credential.ContentSha256 = hex.EncodeToString(digest[:])
	if err := os.Chmod(filepath.Join(root, "token-key"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.readInvocationCredential(t.Context(), credential); err == nil {
		t.Fatal("writable credential accepted")
	}
}

func TestCompleteCatalogueDoesNotCrossExecutorOwnership(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	if !definitions["mattermost"].ExecutableBy(integrationpackage.OwnerInteractionGateway, integrationpackage.RouteInteraction) || definitions["mattermost"].ExecutableBy(integrationpackage.OwnerIntegrationGateway, integrationpackage.RouteManagedMCP) {
		t.Fatal("Mattermost executor ownership changed")
	}
}

func testChannel() *model.Channel {
	return &model.Channel{Id: testChannelID, TeamId: testTeamID, Name: "channel"}
}
