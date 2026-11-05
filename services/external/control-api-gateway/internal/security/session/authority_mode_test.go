package session

import (
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/oidcverifier"
)

// TestRefreshCannotChangeAuthorityMode requires a fresh login rather than accepting a numerically colliding organization/session revision from another authority contract.
// TestRefreshCannotChangeAuthorityMode требует fresh login вместо приёма численно совпавших organization/session revision из другого authority contract.
func TestRefreshCannotChangeAuthorityMode(t *testing.T) {
	legacy := oidcverifier.Principal{Issuer: "https://identity.example.invalid/realm/synthetic", Subject: "synthetic-subject", OrganizationID: "synthetic-organization",
		SessionID: "synthetic-session", SessionRevision: 1, AuthenticatedAt: time.Unix(1, 0)}
	platform := legacy
	platform.PlatformManagedOIDC = true
	if sameSession(legacy, platform) || sameSession(platform, legacy) || !sameSession(platform, platform) || !sameSession(legacy, legacy) {
		t.Fatal("refresh crossed the explicitly selected authority-mode boundary")
	}
}
