package mattermost

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/mattermost/mattermost/server/public/model"
	"google.golang.org/protobuf/proto"
)

func systemDeliveryFixture(t *testing.T, adapter *Adapter) *cp.InteractionDeliveryClaim {
	t.Helper()
	definition, raw := managedPackageFixture(t, adapter.definition, "UI", false)
	return &cp.InteractionDeliveryClaim{DeliveryRef: "delivery_fixture", ConnectionRef: "connection_fixture", ConnectionVersion: 7,
		DefinitionKey: definition.Metadata.Key, DefinitionVersion: definition.Metadata.Version, DefinitionDigest: definition.Digest, DefinitionPackage: raw,
		BaseUrl: "https://chat.example.test", TeamName: "team", ChannelName: "channel", Locale: "en", MessageKey: "READY",
		CapabilityKey: "mattermost.notifications", SourceCapabilityKey: "mattermost.notifications", ApprovalGateRef: "gate_fixture", ApprovalGateVersion: 2}
}

func TestManagedSystemDeliveryChecksApprovalBeforeProviderAndUsesBudget(t *testing.T) {
	adapter, _ := claimFixture(t, "mattermost.notifications", map[string]any{"message": "Ready"})
	claim := systemDeliveryFixture(t, adapter)
	posts := 0
	providerCalls := 0
	claim.CredentialDescriptor = configureProviderFixture(t, adapter, func(request *http.Request) (*http.Response, bool) {
		providerCalls++
		deadline, ok := request.Context().Deadline()
		if !ok || time.Until(deadline) > time.Second {
			t.Fatal("system delivery ignored managed execution budget")
		}
		if request.Method != http.MethodPost || request.URL.Path != "/api/v4/posts" {
			return nil, false
		}
		posts++
		var post model.Post
		if json.NewDecoder(request.Body).Decode(&post) != nil || post.Message != "Ready" || post.ChannelId != testChannelID {
			t.Fatal("system delivery changed approved intent")
		}
		post.Id = testPostID
		return jsonResponse(t, http.StatusCreated, &post), true
	})
	for _, mode := range []string{"missing_package", "digest", "missing_approval", "unresolved_approval", "wrong_source", "missing_connection_version"} {
		t.Run(mode, func(t *testing.T) {
			invalid := proto.Clone(claim).(*cp.InteractionDeliveryClaim)
			switch mode {
			case "missing_package":
				invalid.DefinitionPackage = nil
			case "digest":
				invalid.DefinitionDigest = adapter.definition.Digest
			case "missing_approval":
				invalid.ApprovalGateRef = ""
			case "unresolved_approval":
				invalid.ApprovalGateVersion = 1
			case "wrong_source":
				invalid.SourceCapabilityKey = "mattermost.inbound"
			case "missing_connection_version":
				invalid.ConnectionVersion = 0
			}
			if _, err := adapter.Deliver(t.Context(), invalid); err == nil || !ConfirmedNoEffect(err) || providerCalls != 0 {
				t.Fatal("unapproved or detached intent reached provider")
			}
		})
	}
	if result, err := adapter.Deliver(t.Context(), claim); err != nil || result.PostRef != testPostID || posts != 1 {
		t.Fatalf("approved managed delivery failed: %v", err)
	}
}

func TestManagedSourceRejectsGatedInboundAndDetachedConfiguration(t *testing.T) {
	adapter, _ := claimFixture(t, "mattermost.team.read", map[string]any{})
	definition, raw := managedPackageFixture(t, adapter.definition, "GIT", false)
	original := &cp.InteractionSource{ConnectionRef: "connection_fixture", ConnectionVersion: 7, BaseUrl: "https://chat.example.test", TeamName: "team", ChannelName: "channel",
		DefinitionKey: definition.Metadata.Key, DefinitionVersion: definition.Metadata.Version, DefinitionDigest: definition.Digest, DefinitionPackage: raw,
		EnabledCapabilities: []string{"mattermost.inbound", "mattermost.gate_decisions"}}
	if budget, err := adapter.sourceBudget(original); err != nil || budget != time.Second {
		t.Fatalf("managed source budget rejected: %v", err)
	}
	for _, decision := range []cp.OwnerGateDecision{cp.OwnerGateDecision_OWNER_GATE_DECISION_APPROVE, cp.OwnerGateDecision_OWNER_GATE_DECISION_REQUEST_CHANGES} {
		err := adapter.validateSourceInput(original, "mattermost.gate_decisions", "gate_fixture", decision)
		if (err == nil) != (decision == cp.OwnerGateDecision_OWNER_GATE_DECISION_APPROVE) {
			t.Fatal("source decision ignored actual input schema")
		}
	}
	for _, mode := range []string{"missing_package", "missing_pin", "effect_capability", "duplicate", "bad_config", "gated_inbound"} {
		t.Run(mode, func(t *testing.T) {
			source := proto.Clone(original).(*cp.InteractionSource)
			switch mode {
			case "missing_package":
				source.DefinitionPackage = nil
			case "missing_pin":
				source.DefinitionVersion = ""
			case "effect_capability":
				source.EnabledCapabilities = []string{"mattermost.notifications"}
			case "duplicate":
				source.EnabledCapabilities = []string{"mattermost.inbound", "mattermost.inbound"}
			case "bad_config":
				source.TeamName = ""
			case "gated_inbound":
				var candidate integrationpackage.Package
				if json.Unmarshal(raw, &candidate) != nil {
					t.Fatal("invalid source fixture")
				}
				for index := range candidate.Spec.Capabilities {
					if candidate.Spec.Capabilities[index].Key == "mattermost.inbound" {
						candidate.Spec.Capabilities[index].ApprovalPolicy = "HUMAN_EACH_EFFECT"
					}
				}
				encoded, err := json.Marshal(candidate)
				if err != nil {
					t.Fatal(err)
				}
				parsed, err := integrationpackage.Parse(encoded)
				if err != nil {
					t.Fatal(err)
				}
				source.DefinitionPackage, source.DefinitionDigest = encoded, parsed.Digest
			}
			if _, err := adapter.sourceBudget(source); err == nil {
				t.Fatal("unsafe subscription accepted")
			}
		})
	}
}
