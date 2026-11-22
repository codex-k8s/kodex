package httptransport

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/proto"
	"strings"
	"testing"
)

// TestKubernetesSetupHTTPRequirementsPublicProjection keeps pre-Begin output safe and validates current descriptive pins.
// TestKubernetesSetupHTTPRequirementsPublicProjection сохраняет safe pre-Begin output и проверяет descriptive pins.
func TestKubernetesSetupHTTPRequirementsPublicProjection(t *testing.T) {
	base := &cp.KubernetesIdentitySetupRequirements{TargetRef: "ktg_fixture01", TargetRevision: 2, CanonicalContext: "operator-context", ConnectionVersion: 7, CredentialPolicy: cp.IntegrationCredentialPolicy_INTEGRATION_CREDENTIAL_POLICY_INITIATOR_REQUIRED}
	for _, mode := range []string{"valid", "context-empty", "context-control", "context-utf8", "context-size", "target", "revision", "connection", "policy"} {
		t.Run(mode, func(t *testing.T) {
			current := proto.Clone(base).(*cp.KubernetesIdentitySetupRequirements)
			switch mode {
			case "context-empty":
				current.CanonicalContext = ""
			case "context-control":
				current.CanonicalContext = "context\nforeign"
			case "context-utf8":
				current.CanonicalContext = string([]byte{0xff})
			case "context-size":
				current.CanonicalContext = strings.Repeat("я", 129)
			case "target":
				current.TargetRef = "bad"
			case "revision":
				current.TargetRevision = 0
			case "connection":
				current.ConnectionVersion = 0
			case "policy":
				current.CredentialPolicy = cp.IntegrationCredentialPolicy_INTEGRATION_CREDENTIAL_POLICY_UNSPECIFIED
			}
			body, err := ProtoMap(current)
			if mode == "valid" {
				if err != nil || len(body) != 5 || body["canonicalContext"] != "operator-context" || body["credentialPolicy"] != "INITIATOR_REQUIRED" {
					t.Fatal("safe requirements projection lost exact metadata")
				}
			} else if err == nil {
				t.Fatal("invalid requirements reached public output")
			}
		})
	}
	for _, personal := range []bool{false, true} {
		var message proto.Message = &cp.IntegrationConnection{CredentialPolicy: base.CredentialPolicy, KubernetesSetupRequirements: base}
		if personal {
			message = &cp.PersonalIntegrationConnection{ConnectionRef: "int_fixture01", ConnectionVersion: 7, Name: "Kubernetes", DefinitionKey: "kubernetes-managed", Enabled: true, Available: true, Policy: base.CredentialPolicy, KubernetesSetupRequirements: base}
		}
		body, err := ProtoMap(message)
		if err != nil || body["kubernetesSetupRequirements"] == nil {
			t.Fatal("scoped connection lost pre-Begin requirements")
		}
	}
}

// TestKubernetesSetupHTTPCanonicalContextPublicBoundary rejects malformed immutable setup readback.
// TestKubernetesSetupHTTPCanonicalContextPublicBoundary отвергает malformed immutable setup readback.
func TestKubernetesSetupHTTPCanonicalContextPublicBoundary(t *testing.T) {
	for _, context := range []string{"", "bad\ncontext", strings.Repeat("я", 129), string([]byte{0xff})} {
		current := kubernetesSetupHTTPFixture()
		current.CanonicalContext = context
		if _, ok := kubernetesSetupPublicView(current, current.ConnectionRef); ok {
			t.Fatal("invalid canonical context reached setup output")
		}
	}
	current := kubernetesSetupHTTPFixture()
	current.CanonicalContext = "team'$(synthetic)"
	view, ok := kubernetesSetupPublicView(current, current.ConnectionRef)
	if !ok || view.CanonicalContext != current.CanonicalContext {
		t.Fatal("opaque context was rewritten")
	}
}
