package session

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/oidcverifier"
)

type loginProviderFixture struct {
	state     string
	nonce     string
	challenge string
	fresh     bool
	calls     int
	err       error
	tokens    oidcverifier.BrowserTokens
}

func (p *loginProviderFixture) AuthorizationURL(state, nonce, challenge string, fresh bool) (string, error) {
	p.state, p.nonce, p.challenge, p.fresh = state, nonce, challenge, fresh
	return "https://identity.example.test/authorize", nil
}

func (p *loginProviderFixture) ExchangeCode(_ context.Context, _, verifier, nonce string) (oidcverifier.BrowserTokens, error) {
	p.calls++
	if len(verifier) != 43 || nonce != p.nonce {
		return oidcverifier.BrowserTokens{}, errors.New("PKCE or nonce mismatch")
	}
	return p.tokens, p.err
}

func TestLoginRequiresBrowserBindingBeforeOneTimeExchange(t *testing.T) {
	t.Parallel()
	families, _, tokens := familyFixture(t, refreshFunc(func(context.Context, string) (oidcverifier.BrowserTokens, error) {
		return oidcverifier.BrowserTokens{}, nil
	}))
	provider := &loginProviderFixture{tokens: tokens}
	logins, err := NewLogins(t.Context(), families.store, provider, families.keys)
	if err != nil {
		t.Fatal(err)
	}
	_, cookie, err := logins.Start(t.Context(), nil, true)
	if err != nil {
		t.Fatal(err)
	}
	id, binding, ok := strings.Cut(cookie, ".")
	if !ok || !provider.fresh {
		t.Fatal("fresh login or browser binding missing")
	}
	for _, input := range []struct{ binding, state string }{{"wrong", provider.state}, {binding, "wrong"}} {
		if _, _, err := logins.Exchange(t.Context(), id, input.binding, input.state, "code"); !errors.Is(err, ErrReauthentication) {
			t.Fatal("unbound callback accepted")
		}
	}
	if provider.calls != 0 {
		t.Fatal("unbound callback invoked provider")
	}
	transaction, _, err := logins.Exchange(t.Context(), id, binding, provider.state, "code")
	if err != nil || provider.calls != 1 {
		t.Fatal("valid exchange did not run exactly once")
	}
	if _, _, err := logins.Exchange(t.Context(), id, binding, provider.state, "code"); !errors.Is(err, ErrReauthentication) {
		t.Fatal("uncommitted exchange was repeated")
	}
	family, csrf, err := families.CreateWithCSRF(t.Context(), tokens, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := logins.Complete(t.Context(), transaction, family.ID, csrf); err != nil {
		t.Fatal(err)
	}
	replay, replayTokens, err := logins.Exchange(t.Context(), id, binding, provider.state, "code")
	if err != nil || replay.FamilyID != family.ID || replay.CSRF != csrf || replayTokens.AccessToken != "" || provider.calls != 1 {
		t.Fatal("completed callback did not use protected readback")
	}
}

func TestLoginUncertainOutcomeIsNotReplayedAfterRestart(t *testing.T) {
	t.Parallel()
	families, _, _ := familyFixture(t, refreshFunc(func(context.Context, string) (oidcverifier.BrowserTokens, error) {
		return oidcverifier.BrowserTokens{}, nil
	}))
	provider := &loginProviderFixture{err: oidcverifier.ErrExchangeUncertain}
	logins, err := NewLogins(t.Context(), families.store, provider, families.keys)
	if err != nil {
		t.Fatal(err)
	}
	_, cookie, err := logins.Start(t.Context(), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	id, binding, _ := strings.Cut(cookie, ".")
	if _, _, err := logins.Exchange(t.Context(), id, binding, provider.state, "code"); !errors.Is(err, ErrReauthentication) {
		t.Fatal("uncertain exchange accepted")
	}
	restarted, err := NewLogins(t.Context(), families.store, provider, families.keys)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := restarted.Exchange(t.Context(), id, binding, provider.state, "code"); !errors.Is(err, ErrReauthentication) || provider.calls != 1 {
		t.Fatal("restart repeated authorization code")
	}
}
