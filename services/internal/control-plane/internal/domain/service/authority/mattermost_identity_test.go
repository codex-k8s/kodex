package authority

import (
	"strings"
	"testing"

	authoritytype "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/authority"
)

func TestValidateInteractionGatewayIdentityBinding(t *testing.T) {
	base := authoritytype.ApplicationIdentity{
		ProducerID:           "control-plane.interaction-gateway",
		CredentialPurpose:    "MATTERMOST_SIGNED_EVENT",
		CredentialGeneration: 1,
		ActorID:              "10000000-0000-4000-8000-000000000001",
		OrganizationID:       "10000000-0000-4000-8000-000000000002",
		ProjectID:            "10000000-0000-4000-8000-000000000003",
		SessionJTI:           "10000000-0000-4000-8000-000000000004",
		SessionRevision:      1,
		SubjectDigest:        strings.Repeat("a", 64),
		CredentialDigest:     strings.Repeat("b", 64),
		CallerWorkload:       "interaction-gateway",
	}
	if err := validateApplicationIdentity(base); err != nil {
		t.Fatalf("unbound Mattermost event identity rejected: %v", err)
	}

	partial := base
	partial.BoundTurnID = "10000000-0000-4000-8000-000000000005"
	if err := validateApplicationIdentity(partial); err == nil {
		t.Fatal("partial interaction grant binding accepted")
	}

	bound := partial
	bound.BoundSessionID = "10000000-0000-4000-8000-000000000006"
	bound.BoundAttempt = 1
	bound.BoundInputSHA256 = strings.Repeat("c", 64)
	bound.BoundGeneration = 1
	if err := validateApplicationIdentity(bound); err != nil {
		t.Fatalf("complete interaction grant binding rejected: %v", err)
	}
}

func TestCredentialMatchesExactProducerAndPurpose(t *testing.T) {
	const spiffe = "spiffe://mattercodex.local/ns/mattercodex-system/sa/shared"
	for _, test := range []struct {
		name          string
		workload      string
		producer      string
		purpose       string
		otherProducer string
		otherPurpose  string
	}{
		{"interaction", "interaction-gateway", "control-plane.interaction-gateway", "MATTERMOST_SIGNED_EVENT", "control-plane.owner-gate-delivery", "OWNER_GATE_DELIVERY_GRANT"},
		{"agent runner", "agent-runner", "control-plane.agent-session", "AGENT_SESSION_GRANT", "control-plane.agent-result-access", "INTEGRATION_RESULT_ACCESS_GRANT"},
		{"integration", "integration-gateway", "control-plane.integration-gateway", "AGENT_SESSION_GRANT", "control-plane.integration-continuation", "INTEGRATION_CONTINUATION_GRANT"},
	} {
		t.Run(test.name, func(t *testing.T) {
			operation := Operation{ProducerID: test.producer, CredentialPurpose: test.purpose, CallerWorkload: test.workload, CallerSPIFFEID: spiffe}
			identity := authoritytype.ApplicationIdentity{ProducerID: test.producer, CredentialPurpose: test.purpose, CredentialGeneration: 1, CallerWorkload: test.workload, CallerSPIFFEID: spiffe}
			if !credentialMatches(operation, identity) {
				t.Fatal("exact producer credential rejected")
			}
			identity.ProducerID = test.otherProducer
			identity.CredentialPurpose = test.otherPurpose
			if credentialMatches(operation, identity) {
				t.Fatal("credential from another producer accepted")
			}
		})
	}
}
