package boundary

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/session"
	"github.com/google/uuid"
)

func TestOrganizationRevealConsumesOnlyExactScopedElevation(t *testing.T) {
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	original := session.Claims{Subject: uuid.NewString(), OrganizationID: uuid.NewString(), OIDCSessionID: uuid.NewString(), SessionRevision: 3,
		SessionID: uuid.NewString(), Bearer: "bearer", ExpiresAt: now.Add(time.Hour).Unix(),
		Elevation: &session.Elevation{Kind: session.ElevationKindRuntimeSecretReveal, ScopeKind: "ORGANIZATION", OrganizationRef: "org_example", SecretRef: "sec_example", ExpiresAt: now.Add(time.Minute).Unix()}}
	replacement := original
	replacement.SessionID, replacement.Elevation = uuid.NewString(), nil
	store := &fakeSessionStore{issued: replacement, issuedCode: "replacement-session", issuedCSRF: strings.Repeat("d", 43)}
	revocations := &fakeRevocationStore{consumeWon: true}
	security := testBoundaryWithRevocations(t, &fakeOIDCVerifier{}, store, revocations)
	security.now = func() time.Time { return now }
	ctx := context.WithValue(context.Background(), identityContextKey{}, Identity{BrowserSessionID: original.SessionID, Elevation: original.Elevation})
	ctx = context.WithValue(ctx, authenticatedSessionContextKey{}, authenticatedSession{claims: original, bearerExpiry: now.Add(time.Hour)})
	for _, target := range []struct{ scope, organization, project, secret string }{
		{"PROJECT", "org_example", "prj_example", "sec_example"},
		{"ORGANIZATION", "org_other", "", "sec_example"},
		{"ORGANIZATION", "org_example", "prj_example", "sec_example"},
		{"ORGANIZATION", "org_example", "", "sec_other"},
		{"", "org_example", "", "sec_example"},
	} {
		if err := security.ConsumeRuntimeSecretReveal(ctx, httptest.NewRecorder(), target.scope, target.organization, target.project, target.secret); !errors.Is(err, ErrElevationRequired) {
			t.Fatalf("mismatched owner tuple accepted: %v", err)
		}
	}
	if revocations.consumed != "" || store.issueCalls != 0 {
		t.Fatal("invalid scoped target consumed the one-time elevation")
	}
	if err := security.ConsumeRuntimeSecretReveal(ctx, httptest.NewRecorder(), "ORGANIZATION", "org_example", "", "sec_example"); err != nil {
		t.Fatal(err)
	}
	if revocations.consumed != original.SessionID || store.issueCalls != 1 {
		t.Fatal("exact organization target did not consume elevation")
	}
	revocations.consumeWon = false
	if err := security.ConsumeRuntimeSecretReveal(ctx, httptest.NewRecorder(), "ORGANIZATION", "org_example", "", "sec_example"); !errors.Is(err, ErrElevationConsumed) {
		t.Fatalf("organization elevation replay accepted: %v", err)
	}
}
