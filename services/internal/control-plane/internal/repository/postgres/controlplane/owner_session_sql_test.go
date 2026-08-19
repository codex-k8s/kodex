package controlplane

import (
	"strings"
	"testing"
)

func TestOwnerSessionAdmissionAcceptsTokenRotationWithinActiveOIDCSession(t *testing.T) {
	t.Parallel()

	for _, required := range []string{
		"EXCLUDED.current_revision = owner_oidc_sessions.current_revision",
		"owner_oidc_sessions.revoked_at IS NULL",
		"EXCLUDED.current_revision = owner_oidc_sessions.current_revision + 1",
		"credential_digest_sha256 = EXCLUDED.credential_digest_sha256",
	} {
		if !strings.Contains(sqlOwnerSessionAdmit, required) {
			t.Fatalf("owner session admission misses token rotation clause %q", required)
		}
	}
	if strings.Contains(sqlOwnerSessionAdmit,
		"EXCLUDED.credential_digest_sha256 = owner_oidc_sessions.credential_digest_sha256") {
		t.Fatal("active OIDC session still rejects a freshly issued access token")
	}
}
