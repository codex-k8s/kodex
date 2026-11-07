package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/oidcverifier"
)

// platformFamilyFixture uses real family cryptography and memory CAS with separately bounded synthetic owner/session authority.
// It models replicas sharing owner records, not broker durability or an installed identity provider.
// platformFamilyFixture использует реальную family cryptography и memory CAS с отдельно ограниченной synthetic owner/session authority.
// Fixture моделирует replicas с общими owner records, а не broker durability и установленный identity provider.
func platformFamilyFixture(t *testing.T, refresh TokenRefresher) (*Families, Family, oidcverifier.BrowserTokens) {
	t.Helper()
	families, _, tokens := familyFixture(t, refresh)
	tokens.Principal.Issuer = "https://identity.example.invalid/realm/synthetic"
	tokens.Principal.PlatformManagedOIDC = true
	tokens.Principal.AuthenticatedAt = families.now().Add(-time.Minute)
	tokens.Principal.PlatformSessionExpiresAt = families.now().Add(time.Hour)
	family, _, err := families.CreateWithCSRF(t.Context(), tokens, nil)
	if err != nil {
		t.Fatal(err)
	}
	return families, family, tokens
}

// TestPlatformFamilyRevokeFenceAndReceipt proves durable-intent rejoin, application/refresh denial and exact confirmed terminal cleanup across replicas.
// Unknown owner outcomes retain only the original access credential; no refresh effect or new session is invented.
// TestPlatformFamilyRevokeFenceAndReceipt проверяет durable-intent rejoin, запрет application/refresh и exact confirmed terminal cleanup между replicas.
// Unknown owner outcome сохраняет лишь исходный access credential; refresh effect и новая session не создаются.
func TestPlatformFamilyRevokeFenceAndReceipt(t *testing.T) {
	calls := 0
	families, family, _ := platformFamilyFixture(t, refreshFunc(func(context.Context, string) (oidcverifier.BrowserTokens, error) {
		calls++
		return oidcverifier.BrowserTokens{}, errors.New("unexpected refresh")
	}))
	if !family.AbsoluteExpiresAt.Equal(family.Principal.PlatformSessionExpiresAt) {
		t.Fatal("family exceeded or ignored the owner session ceiling")
	}
	claims, _, err := families.Cookie(family)
	if err != nil || !claims.PlatformManagedOIDC {
		t.Fatal("sealed cookie lost its authority-mode binding")
	}
	if err := families.CompletePlatformRevoke(t.Context(), family.ID, family.BrowserSessionID, family.CSRFHash, family.Principal.SessionRevision); err == nil {
		t.Fatal("owner receipt was manufactured without a local revoke intent")
	}
	pending, err := families.BeginPlatformRevoke(t.Context(), family.ID, family.BrowserSessionID, family.CSRFHash)
	if err != nil || pending.State != familyRevoking || pending.AccessToken != family.AccessToken || pending.RefreshToken != "" || pending.OwnerRevokeConfirmed {
		t.Fatal("pending owner revoke did not fence refresh or retained the wrong credentials")
	}
	replica, err := NewFamilies(t.Context(), families.store, families.tokens, families.keys)
	if err != nil {
		t.Fatal(err)
	}
	replica.now = families.now
	joined, err := replica.BeginPlatformRevoke(t.Context(), family.ID, family.BrowserSessionID, family.CSRFHash)
	if err != nil || joined.Sequence != pending.Sequence || joined.Principal.SessionRevision != family.Principal.SessionRevision {
		t.Fatal("lost-response rejoin changed the pending identity or version")
	}
	if _, err := replica.Read(t.Context(), family.ID); err == nil {
		t.Fatal("pending revoke admitted ordinary application access")
	}
	if _, err := replica.Renew(t.Context(), family.ID, family.BrowserSessionID, family.CSRFHash); err == nil || calls != 0 {
		t.Fatal("pending revoke called the refresh provider")
	}
	if err := replica.Revoke(t.Context(), family.ID); err == nil {
		t.Fatal("generic revoke bypassed owner confirmation")
	}
	if err := replica.CompletePlatformRevoke(t.Context(), family.ID, family.BrowserSessionID, family.CSRFHash, family.Principal.SessionRevision); err != nil {
		t.Fatal(err)
	}
	terminal, err := families.ReadForClosure(t.Context(), family.ID)
	if err != nil || !terminal.OwnerRevokeConfirmed || terminal.State != familyTerminal || terminal.AccessToken != "" || terminal.RefreshToken != "" {
		t.Fatal("confirmed receipt did not survive replica readback with credentials erased")
	}
	if err := families.CompletePlatformRevoke(t.Context(), family.ID, family.BrowserSessionID, family.CSRFHash, family.Principal.SessionRevision); err != nil {
		t.Fatal("confirmed lost-response repeat was not idempotent")
	}
	if _, err := families.Read(t.Context(), family.ID); err == nil {
		t.Fatal("confirmed terminal receipt reopened application access")
	}
}

// TestPlatformBearerFamilyHasNoRefreshCredential proves API bearer login uses a mode-bound family with a real token ceiling and no dummy provider refresh token.
// TestPlatformBearerFamilyHasNoRefreshCredential проверяет mode-bound family API bearer login с реальным token ceiling без dummy provider refresh token.
func TestPlatformBearerFamilyHasNoRefreshCredential(t *testing.T) {
	calls := 0
	families, _, tokens := platformFamilyFixture(t, refreshFunc(func(context.Context, string) (oidcverifier.BrowserTokens, error) {
		calls++
		return oidcverifier.BrowserTokens{}, nil
	}))
	family, csrf, err := families.CreateBearerWithCSRF(t.Context(), tokens.Principal, tokens.AccessToken, nil)
	if err != nil || csrf == "" || !family.BearerOnly || family.RefreshToken != "" || !family.RefreshExpiresAt.IsZero() || !family.AbsoluteExpiresAt.Equal(tokens.Principal.ExpiresAt) {
		t.Fatal("bearer family invented a refresh credential or extended its token lifetime")
	}
	if _, err := families.Renew(t.Context(), family.ID, family.BrowserSessionID, family.CSRFHash); err != nil || calls != 0 {
		t.Fatal("bearer-only idle renewal contacted the provider")
	}
	families.now = func() time.Time { return tokens.Principal.ExpiresAt.Add(time.Second) }
	if _, err := families.Read(t.Context(), family.ID); err == nil {
		t.Fatal("expired bearer family reopened access")
	}
}

// TestPlatformLogoutWinsOverInflightRefresh races an already reserved provider effect against the durable logout fence and confirmed receipt.
// A late rotated credential cannot replace the terminal record or reopen the original browser family.
// TestPlatformLogoutWinsOverInflightRefresh сталкивает уже reserved provider effect с durable logout fence и confirmed receipt.
// Поздний rotated credential не заменяет terminal record и не переоткрывает browser family.
func TestPlatformLogoutWinsOverInflightRefresh(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var response oidcverifier.BrowserTokens
	var once sync.Once
	defer once.Do(func() { close(release) })
	families, family, tokens := platformFamilyFixture(t, refreshFunc(func(ctx context.Context, _ string) (oidcverifier.BrowserTokens, error) {
		close(started)
		select {
		case <-release:
			return response, nil
		case <-ctx.Done():
			return oidcverifier.BrowserTokens{}, ctx.Err()
		}
	}))
	response = tokens
	response.AccessToken, response.RefreshToken = "synthetic-rotated-access", "synthetic-rotated-refresh"
	response.Principal.ExpiresAt = families.now().Add(10 * time.Minute)
	now := families.now().Add(45 * time.Second)
	families.now = func() time.Time { return now }
	completed := make(chan error, 1)
	go func() {
		_, err := families.Renew(t.Context(), family.ID, family.BrowserSessionID, family.CSRFHash)
		completed <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("refresh did not reserve its provider effect")
	}
	if _, err := families.BeginPlatformRevoke(t.Context(), family.ID, family.BrowserSessionID, family.CSRFHash); err != nil {
		t.Fatal(err)
	}
	if err := families.CompletePlatformRevoke(t.Context(), family.ID, family.BrowserSessionID, family.CSRFHash, family.Principal.SessionRevision); err != nil {
		t.Fatal(err)
	}
	once.Do(func() { close(release) })
	select {
	case err := <-completed:
		if err == nil {
			t.Fatal("late refresh committed across logout")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("late refresh did not finish")
	}
	terminal, err := families.ReadForClosure(t.Context(), family.ID)
	if err != nil || !terminal.OwnerRevokeConfirmed || terminal.AccessToken != "" || terminal.RefreshToken != "" {
		t.Fatal("late refresh resurrected cleared credentials")
	}
}
