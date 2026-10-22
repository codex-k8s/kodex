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

// familyMemory is a mutex-protected in-memory ciphertext/sequence fixture shared by simulated replicas. It implements local atomic CAS, not durable JetStream storage, retention or deployed access controls.
// familyMemory — защищённый mutex in-memory ciphertext и sequence fixture для моделируемых replicas. Реализуется local atomic CAS, а не durable JetStream storage, retention и deployed access controls.
type familyMemory struct {
	mu      sync.Mutex
	records map[string]browserstate.Record
}

// Read returns a cloned ciphertext record under the fixture mutex or ErrNotFound, preventing tests from mutating stored bytes through the result. It deliberately ignores context cancellation.
// Read под fixture mutex возвращает record с клонированным ciphertext либо ErrNotFound, не позволяя тесту менять сохранённые байты через результат. Context cancellation намеренно игнорируется.
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

// CompareAndSwap checks the exact sequence under the mutex, clones supplied ciphertext and increments sequence once on success. This fixture ignores context and has no broker or external persistence.
// CompareAndSwap под mutex проверяет exact sequence, копирует переданный ciphertext и при успехе однократно увеличивает sequence. Fixture игнорирует context и не имеет broker и внешней persistence.
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

// refreshFunc adapts a local callback to TokenRefresher so units can control returned tokens, failure and synchronization without a real OIDC endpoint.
// refreshFunc адаптирует локальный callback к TokenRefresher, позволяя units управлять returned tokens, failure и synchronization без настоящего OIDC endpoint.
type refreshFunc func(context.Context, string) (oidcverifier.BrowserTokens, error)

// Refresh forwards the exact context/token to the configured callback and returns its response/error unchanged; the adapter adds no retry or validation.
// Refresh передаёт точные context и token настроенному callback и возвращает response и error без изменения; retry и validation не добавляются.
func (f refreshFunc) Refresh(ctx context.Context, token string) (oidcverifier.BrowserTokens, error) {
	return f(ctx, token)
}

// familyFixture creates a synthetic AES-GCM key, in-memory CAS store, fixed local clock and trusted fixture token/principal data, then persists one active family. It performs no login, network request or production key read.
// familyFixture создаёт synthetic AES-GCM key, in-memory CAS store, фиксированные локальные часы и доверенные fixture token и principal data, затем сохраняет одно active family. Login, network request и чтение production key не выполняются.
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

// TestFamilyRotationPreservesAbsoluteSSOAndSealsCredentials checks one synthetic refresh preserves the absolute ceiling, advances storage sequence, hides token strings in ciphertext and rejects moving it to another family ID; a second fresh read avoids another provider call.
// TestFamilyRotationPreservesAbsoluteSSOAndSealsCredentials проверяет сохранение absolute ceiling одним synthetic refresh, рост storage sequence, отсутствие token strings в ciphertext и отказ переноса к другому family ID; повторный свежий доступ не вызывает provider.
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

// TestFamilyRefreshCannotCompletePastSessionDeadline advances a fake clock to idle or absolute expiry during the provider callback and checks rejection plus persisted terminal credential removal. The provider and store are local fixtures.
// TestFamilyRefreshCannotCompletePastSessionDeadline продвигает fake clock к idle либо absolute expiry внутри provider callback и проверяет отказ и сохранённое terminal удаление credentials. Provider и store являются local fixtures.
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

// TestUnknownRefreshAndExpiredAttemptNeverReuseToken injects an uncertain provider outcome and an already expired reserved attempt, requiring reauthentication without a second token exchange. It simulates stored crash state rather than restarting a process.
// TestUnknownRefreshAndExpiredAttemptNeverReuseToken задаёт uncertain provider outcome и уже expired reserved attempt, требуя reauthentication без второго token exchange. Моделируется сохранённое crash state, а не перезапуск процесса.
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

// TestLogoutFencesConcurrentRefreshAcrossReplicas blocks one local refresh callback, observes pending from a second Families value and revokes through their shared memory CAS, then rejects the late commit. It is a concurrency unit, not a deployed multi-Pod durability test.
// TestLogoutFencesConcurrentRefreshAcrossReplicas блокирует один local refresh callback, наблюдает pending из второго Families value и отзывает через общий memory CAS, затем отклоняет late commit. Это concurrency unit, а не deployed multi-Pod durability test.
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

// TestRefreshRejectsChangedSessionAuthority mutates issuer, revision, original authentication time or tenant in synthetic refreshed tokens and requires local renewal refusal. It does not test a real provider's JWT validation.
// TestRefreshRejectsChangedSessionAuthority меняет issuer, revision, исходное authentication time либо tenant synthetic refreshed tokens и требует local renewal refusal без проверки настоящей provider JWT validation.
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

// TestFamilyRenewalAtAbsoluteLimitDoesNotStorm checks renewal scheduling at the absolute boundary and runs forty-two no-op renewals when access covers it, asserting unchanged version/sequence and eventual expiry. The access-before-absolute branch checks scheduling only.
// TestFamilyRenewalAtAbsoluteLimitDoesNotStorm проверяет renewal scheduling у absolute boundary и выполняет 42 no-op renewals при покрывающем её access, требуя неизменных version и sequence и последующего expiry. Ветка access-before-absolute проверяет лишь scheduling.
func TestFamilyRenewalAtAbsoluteLimitDoesNotStorm(t *testing.T) {
	for _, accessBeforeAbsolute := range []bool{false, true} {
		t.Run(map[bool]string{false: "access-covers-absolute", true: "access-still-needs-refresh"}[accessBeforeAbsolute], func(t *testing.T) {
			families, family, _ := familyFixture(t, refreshFunc(func(context.Context, string) (oidcverifier.BrowserTokens, error) {
				t.Fatal("unexpected refresh")
				return oidcverifier.BrowserTokens{}, nil
			}))
			now := families.now()
			family.AbsoluteExpiresAt, family.IdleExpiresAt = now.Add(4*time.Minute), now.Add(4*time.Minute)
			family.Principal.ExpiresAt = family.AbsoluteExpiresAt.Add(time.Minute)
			if accessBeforeAbsolute {
				family.Principal.ExpiresAt = now.Add(3 * time.Minute)
			}
			family, err := families.write(t.Context(), family)
			if err != nil {
				t.Fatal(err)
			}
			expected := family.AbsoluteExpiresAt
			if accessBeforeAbsolute {
				expected = now.Add(time.Minute)
			}
			if !families.RenewAfter(family).Equal(expected) {
				t.Fatal("no-op idle renewal scheduled before absolute expiry")
			}
			if !accessBeforeAbsolute {
				for range 42 {
					renewed, err := families.Renew(t.Context(), family.ID, family.BrowserSessionID, family.CSRFHash)
					if err != nil || renewed.Version != family.Version || renewed.Sequence != family.Sequence {
						t.Fatal("no-op refresh mutated session", err)
					}
				}
				families.now = func() time.Time { return family.AbsoluteExpiresAt }
				if _, err := families.Read(t.Context(), family.ID); !errors.Is(err, ErrReauthentication) {
					t.Fatal("absolute expiry was extended")
				}
			}
		})
	}
}
