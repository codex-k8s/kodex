package authority

import (
	"strings"
	"testing"

	authoritytype "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/authority"
)

func TestIdentityProvenanceForControlAPIGateway(t *testing.T) {
	const (
		sessionJTI = "10000000-0000-4000-8000-000000000001"
		sessionID  = "10000000-0000-4000-8000-000000000002"
	)
	operation := Operation{AuthoritySource: "WORKLOAD_READINESS"}
	identity := authoritytype.ApplicationIdentity{
		CallerWorkload:   "control-api-gateway",
		SessionJTI:       sessionJTI,
		SessionRevision:  1,
		CredentialDigest: strings.Repeat("a", 64),
	}

	t.Run("readiness grant keeps session JTI", func(t *testing.T) {
		provenance := identityProvenance(operation, identity)
		if provenance.Reference != sessionJTI {
			t.Fatalf("provenance reference = %q, want session JTI %q", provenance.Reference, sessionJTI)
		}
	})

	t.Run("owner session uses session ID", func(t *testing.T) {
		withSession := identity
		withSession.SessionID = sessionID
		provenance := identityProvenance(operation, withSession)
		if provenance.Reference != sessionID {
			t.Fatalf("provenance reference = %q, want session ID %q", provenance.Reference, sessionID)
		}
	})
}
