package authority

import (
	"strings"
	"testing"

	authoritytype "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/authority"
)

func TestValidateInteractionGatewayIdentityBinding(t *testing.T) {
	base := authoritytype.ApplicationIdentity{
		ActorID:          "10000000-0000-4000-8000-000000000001",
		OrganizationID:   "10000000-0000-4000-8000-000000000002",
		ProjectID:        "10000000-0000-4000-8000-000000000003",
		SessionJTI:       "10000000-0000-4000-8000-000000000004",
		SessionRevision:  1,
		SubjectDigest:    strings.Repeat("a", 64),
		CredentialDigest: strings.Repeat("b", 64),
		CallerWorkload:   "interaction-gateway",
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
