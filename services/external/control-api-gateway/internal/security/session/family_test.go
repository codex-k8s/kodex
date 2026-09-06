package session

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/eventing/browserstate"
	"github.com/codex-k8s/kodex/libs/go/oidcverifier"
)

type familyMemory struct {
	mu      sync.Mutex
	records map[string]browserstate.Record
}

func (s *familyMemory) Read(_ context.Context, id string) (browserstate.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[id]
	if !ok {
		return browserstate.Record{}, browserstate.ErrNotFound
	}
	record.Ciphertext = bytes.Clone(record.Ciphertext)
	return record, nil
}

func (s *familyMemory) CompareAndSwap(_ context.Context, id string, expected uint64, raw []byte) (browserstate.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.records[id].Sequence != expected {
		return browserstate.Record{}, browserstate.ErrConflict
	}
	record := browserstate.Record{Sequence: expected + 1, Ciphertext: bytes.Clone(raw)}
	s.records[id] = record
	return record, nil
}

type refreshFunc func(context.Context, string) (oidcverifier.BrowserTokens, error)

func (f refreshFunc) Refresh(ctx context.Context, token string) (oidcverifier.BrowserTokens, error) {
	return f(ctx, token)
}

func familyFixture(t *testing.T, refresh TokenRefresher) (*Families, Family, oidcverifier.BrowserTokens) {
	t.Helper()
	block, err := aes.NewCipher(bytes.Repeat([]byte{17}, 32))
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	storage := &familyMemory{records: map[string]browserstate.Record{}}
	families, err := NewFamilies(t.Context(), storage, refresh, &Store{current: aead, ttl: 15 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	families.now = func() time.Time { return now }
	tokens := oidcverifier.BrowserTokens{AccessToken: "initial-access", RefreshToken: "initial-refresh", RefreshExpiresAt: now.Add(11 * time.Hour),
		Principal: oidcverifier.Principal{Issuer: "https://identity.example.test/realms/kodex", Subject: "1774ee65-c979-40e9-99ca-0a93a281dcb0",
			OrganizationID: "c1ac47e6-b22e-43bf-a075-64e4b84f1a15", SessionID: "f2a8c6ea-1d59-42e3-b6d6-f070eab6ff27", SessionRevision: 1,
			AuthenticatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(time.Minute)}}
	family, err := families.Create(t.Context(), tokens, strings.Repeat("a", 64), nil)
	if err != nil {
		t.Fatal(err)
	}
	return families, family, tokens
}

func TestFamilyRotationPreservesAbsoluteSSOAndSealsCredentials(t *testing.T) {
	t.Parallel()
	var response oidcverifier.BrowserTokens
	calls := 0
	families, family, tokens := familyFixture(t, refreshFunc(func(context.Context, string) (oidcverifier.BrowserTokens, error) { calls++; return response, nil }))
	response = tokens
	response.AccessToken, response.RefreshToken = "rotated-access", "rotated-refresh"
	response.Principal.ExpiresAt = families.now().Add(time.Hour)
	response.RefreshExpiresAt = families.now().Add(12 * time.Hour)
	renewed, err := families.Renew(t.Context(), family.ID, family.BrowserSessionID, family.CSRFHash)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !renewed.AbsoluteExpiresAt.Equal(family.AbsoluteExpiresAt) || renewed.Sequence <= family.Sequence || renewed.AccessToken != response.AccessToken {
		t.Fatal("rotation lost immutable session bounds")
	}
	record, err := families.store.Read(t.Context(), family.ID)
	if err != nil || bytes.Contains(record.Ciphertext, []byte(response.RefreshToken)) || bytes.Contains(record.Ciphertext, []byte(response.AccessToken)) {
		t.Fatal("credentials were not sealed")
	}
	if _, err := families.decode("f298e4d2-4da3-48f2-8782-79539a3a6a12", record); !errors.Is(err, ErrReauthentication) {
		t.Fatal("ciphertext moved to another family")
	}
	if _, err := families.Renew(t.Context(), family.ID, family.BrowserSessionID, family.CSRFHash); err != nil || calls != 1 {
		t.Fatal("fresh access token triggered another provider effect")
	}
}

func TestFamilyRefreshCannotCompletePastSessionDeadline(t *testing.T) {
	for _, absolute := range []bool{false, true} {
		t.Run(map[bool]string{false: "idle", true: "absolute"}[absolute], func(t *testing.T) {
			var families *Families
			var response oidcverifier.BrowserTokens
			var deadline time.Time
			families, family, tokens := familyFixture(t, refreshFunc(func(context.Context, string) (oidcverifier.BrowserTokens, error) {
				families.now = func() time.Time { return deadline }
				return response, nil
			}))
			deadline = family.IdleExpiresAt
			if absolute {
				deadline = family.AbsoluteExpiresAt
			}
			response = tokens
			response.Principal.ExpiresAt = deadline.Add(time.Hour)
			response.RefreshExpiresAt = deadline.Add(time.Hour)
			if _, err := families.Renew(t.Context(), family.ID, family.BrowserSessionID, family.CSRFHash); !errors.Is(err, ErrReauthentication) {
				t.Fatal("refresh revived an expired session")
			}
			record, err := families.store.Read(t.Context(), family.ID)
			if err != nil {
				t.Fatal(err)
			}
			closed, err := families.decode(family.ID, record)
			if err != nil || closed.State != familyTerminal || closed.AccessToken != "" || closed.RefreshToken != "" {
				t.Fatal("expired refresh retained usable credentials")
			}
		})
	}
}

func TestUnknownRefreshAndExpiredAttemptNeverReuseToken(t *testing.T) {
	t.Parallel()
	calls := 0
	families, family, _ := familyFixture(t, refreshFunc(func(context.Context, string) (oidcverifier.BrowserTokens, error) {
		calls++
		return oidcverifier.BrowserTokens{}, oidcverifier.ErrExchangeUncertain
	}))
	for range 2 {
		if _, err := families.Renew(t.Context(), family.ID, family.BrowserSessionID, family.CSRFHash); !errors.Is(err, ErrReauthentication) {
			t.Fatal("uncertain refresh was accepted")
		}
	}
	if calls != 1 {
		t.Fatal("uncertain refresh was repeated")
	}
	other, _, tokens := familyFixture(t, refreshFunc(func(context.Context, string) (oidcverifier.BrowserTokens, error) {
		t.Error("crashed attempt retried provider")
		return oidcverifier.BrowserTokens{}, nil
	}))
	crashed, err := other.Create(t.Context(), tokens, strings.Repeat("a", 64), nil)
	if err != nil {
		t.Fatal(err)
	}
	crashed.State, crashed.AttemptID = familyRefreshing, "af762e11-52ef-40ac-aeff-c776497655af"
	crashed.RefreshDeadline = other.now().Add(-time.Second)
	crashed.Version++
	if _, err := other.write(t.Context(), crashed); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Renew(t.Context(), crashed.ID, crashed.BrowserSessionID, crashed.CSRFHash); !errors.Is(err, ErrReauthentication) {
		t.Fatal("crashed attempt recovered by reusing grant")
	}
}

func TestLogoutFencesConcurrentRefreshAcrossReplicas(t *testing.T) {
	t.Parallel()
	started, release := make(chan struct{}), make(chan struct{})
	var response oidcverifier.BrowserTokens
	families, family, tokens := familyFixture(t, refreshFunc(func(ctx context.Context, _ string) (oidcverifier.BrowserTokens, error) {
		close(started)
		select {
		case <-release:
			return response, nil
		case <-ctx.Done():
			return oidcverifier.BrowserTokens{}, ctx.Err()
		}
	}))
	response = tokens
	response.Principal.ExpiresAt = families.now().Add(time.Hour)
	result := make(chan error, 1)
	go func() {
		_, err := families.Renew(t.Context(), family.ID, family.BrowserSessionID, family.CSRFHash)
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("refresh did not start")
	}
	replica := *families
	if _, err := replica.Renew(t.Context(), family.ID, family.BrowserSessionID, family.CSRFHash); !errors.Is(err, ErrRenewalPending) {
		t.Fatal("second replica acquired refresh")
	}
	if err := replica.Revoke(t.Context(), family.ID); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-result; !errors.Is(err, browserstate.ErrConflict) {
		t.Fatal("late refresh bypassed logout")
	}
	if _, err := families.Read(t.Context(), family.ID); !errors.Is(err, ErrReauthentication) {
		t.Fatal("logout did not remain terminal")
	}
}

func TestRefreshRejectsChangedSessionAuthority(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*oidcverifier.Principal){
		"issuer":         func(p *oidcverifier.Principal) { p.Issuer += "/other" },
		"revision":       func(p *oidcverifier.Principal) { p.SessionRevision++ },
		"authentication": func(p *oidcverifier.Principal) { p.AuthenticatedAt = p.AuthenticatedAt.Add(time.Second) },
		"tenant":         func(p *oidcverifier.Principal) { p.OrganizationID = "3a1a701c-c7eb-4294-b071-38d78b79b4ee" },
	} {
		t.Run(name, func(t *testing.T) {
			var response oidcverifier.BrowserTokens
			families, family, tokens := familyFixture(t, refreshFunc(func(context.Context, string) (oidcverifier.BrowserTokens, error) { return response, nil }))
			response = tokens
			mutate(&response.Principal)
			if _, err := families.Renew(t.Context(), family.ID, family.BrowserSessionID, family.CSRFHash); !errors.Is(err, ErrReauthentication) {
				t.Fatal("refresh changed session authority")
			}
		})
	}
}
