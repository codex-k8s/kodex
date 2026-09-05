package boundary

import (
	"errors"
	"strings"
	"testing"
	"time"

	oidcauth "github.com/codex-k8s/kodex/libs/go/oidcverifier"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/session"
	"github.com/google/uuid"
)

func TestIssueEmailSessionBindsFreshIdentityAndReceipt(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name                    string
		age, bearerTTL, wantTTL time.Duration
	}{
		{"fresh", 0, time.Hour, 2 * time.Minute},
		{"nearly-stale", 270 * time.Second, time.Hour, 30 * time.Second},
		{"bearer-ceiling", 0, 90 * time.Second, 90 * time.Second},
		{"future-skew", -30 * time.Second, time.Hour, 2 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeSessionStore{}
			security := testBoundary(t, &fakeOIDCVerifier{}, store)
			security.now = func() time.Time { return now }
			principal := oidcauth.Principal{Subject: uuid.NewString(), OrganizationID: uuid.NewString(), SessionID: uuid.NewString(), SessionRevision: 2, ExpiresAt: now.Add(tc.bearerTTL), AuthenticatedAt: now.Add(-tc.age), ACR: "1", AMR: []string{"pwd"}}
			purpose := &SessionPurpose{Kind: session.ElevationKindEmailReconciliation, ReceiptRef: "erc_fixture01", ReceiptVersion: 3, ReceiptDigest: strings.Repeat("a", 64)}
			_, _, _, err := security.IssueSession(principal, "fixture-bearer", purpose)
			e := store.elevation
			if err != nil || store.issueCalls != 0 || store.elevationIssueCalls != 1 || e == nil || e.Kind != purpose.Kind || e.ReceiptRef != purpose.ReceiptRef || e.ReceiptVersion != 3 || e.ReceiptDigest != purpose.ReceiptDigest || e.ProjectRef != "" || e.SecretRef != "" || e.ExpiresAt != now.Add(tc.wantTTL).Unix() {
				t.Fatalf("email session binding: err=%v elevation=%+v", err, e)
			}
		})
	}
}

func TestIssueEmailSessionRejectsStaleIdentityAndInvalidPurpose(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		mutate func(*oidcauth.Principal, *SessionPurpose)
		want   error
	}{
		{"missing-auth", func(p *oidcauth.Principal, _ *SessionPurpose) { p.AuthenticatedAt = time.Time{} }, ErrFreshAuthenticationRequired},
		{"stale-auth", func(p *oidcauth.Principal, _ *SessionPurpose) { p.AuthenticatedAt = now.Add(-5 * time.Minute) }, ErrFreshAuthenticationRequired},
		{"future-auth", func(p *oidcauth.Principal, _ *SessionPurpose) { p.AuthenticatedAt = now.Add(31 * time.Second) }, ErrFreshAuthenticationRequired},
		{"missing-acr", func(p *oidcauth.Principal, _ *SessionPurpose) { p.ACR = " " }, ErrFreshAuthenticationRequired},
		{"missing-amr", func(p *oidcauth.Principal, _ *SessionPurpose) { p.AMR = nil }, ErrFreshAuthenticationRequired},
		{"empty-amr", func(p *oidcauth.Principal, _ *SessionPurpose) { p.AMR = []string{" "} }, ErrFreshAuthenticationRequired},
		{"mixed-project", func(_ *oidcauth.Principal, p *SessionPurpose) { p.ProjectRef = "prj_fixture01" }, ErrSessionPurposeInvalid},
		{"mixed-secret", func(_ *oidcauth.Principal, p *SessionPurpose) { p.SecretRef = "sec_fixture01" }, ErrSessionPurposeInvalid},
		{"wrong-kind", func(_ *oidcauth.Principal, p *SessionPurpose) { p.Kind = session.ElevationKindRuntimeSecretReveal }, ErrSessionPurposeInvalid},
		{"unknown-kind", func(_ *oidcauth.Principal, p *SessionPurpose) { p.Kind = "UNKNOWN" }, ErrSessionPurposeInvalid},
		{"missing-ref", func(_ *oidcauth.Principal, p *SessionPurpose) { p.ReceiptRef = "" }, ErrSessionPurposeInvalid},
		{"missing-version", func(_ *oidcauth.Principal, p *SessionPurpose) { p.ReceiptVersion = 0 }, ErrSessionPurposeInvalid},
		{"missing-digest", func(_ *oidcauth.Principal, p *SessionPurpose) { p.ReceiptDigest = "" }, ErrSessionPurposeInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			principal := oidcauth.Principal{Subject: uuid.NewString(), OrganizationID: uuid.NewString(), SessionID: uuid.NewString(), SessionRevision: 2, ExpiresAt: now.Add(time.Hour), AuthenticatedAt: now, ACR: "1", AMR: []string{"pwd"}}
			purpose := SessionPurpose{Kind: session.ElevationKindEmailReconciliation, ReceiptRef: "erc_fixture01", ReceiptVersion: 3, ReceiptDigest: strings.Repeat("a", 64)}
			tc.mutate(&principal, &purpose)
			store := &fakeSessionStore{}
			security := testBoundary(t, &fakeOIDCVerifier{}, store)
			security.now = func() time.Time { return now }
			if _, _, _, err := security.IssueSession(principal, "fixture-bearer", &purpose); !errors.Is(err, tc.want) || store.issueCalls != 0 || store.elevationIssueCalls != 0 {
				t.Fatalf("invalid session error=%v", err)
			}
		})
	}
}
