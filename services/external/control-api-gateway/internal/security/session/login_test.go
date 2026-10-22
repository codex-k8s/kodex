package session

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/oidcverifier"
)

// loginProviderFixture captures authorization parameters, supplies a fixed inert URL and counts local exchanges with configurable tokens/error. It has no network client, real authorization code or credential.
// loginProviderFixture сохраняет authorization parameters, выдаёт фиксированный инертный URL и считает local exchanges с настраиваемыми tokens и error. Network client, настоящий authorization code и credential отсутствуют.
type loginProviderFixture struct {
	state     string
	nonce     string
	challenge string
	fresh     bool
	calls     int
	err       error
	tokens    oidcverifier.BrowserTokens
}

// AuthorizationURL records state/nonce/challenge/fresh parameters and returns a reserved example-domain URL without contacting an identity service.
// AuthorizationURL записывает state, nonce, challenge и fresh parameters и возвращает URL зарезервированного example-домена без обращения к identity service.
func (p *loginProviderFixture) AuthorizationURL(state, nonce, challenge string, fresh bool) (string, error) {
	p.state, p.nonce, p.challenge, p.fresh = state, nonce, challenge, fresh
	return "https://identity.example.test/authorize", nil
}

// ExchangeCode increments the fixture counter, checks verifier length and captured nonce, then returns configured tokens/error. It does not recompute the S256 challenge or cryptographically validate an authorization code.
// ExchangeCode увеличивает fixture counter, проверяет длину verifier и сохранённый nonce, затем возвращает настроенные tokens и error. S256 challenge не пересчитывается и authorization code криптографически не проверяется.
func (p *loginProviderFixture) ExchangeCode(_ context.Context, _, verifier, nonce string) (oidcverifier.BrowserTokens, error) {
	p.calls++
	if len(verifier) != 43 || nonce != p.nonce {
		return oidcverifier.BrowserTokens{}, errors.New("PKCE or nonce mismatch")
	}
	return p.tokens, p.err
}

// TestLoginRequiresBrowserBindingBeforeOneTimeExchange rejects wrong browser binding/state before the callback, permits one exchange and verifies completed replay returns stored family/CSRF without tokens or a second provider call. Storage/provider are local fixtures, not a browser/IdP deployment.
// TestLoginRequiresBrowserBindingBeforeOneTimeExchange отклоняет неверные browser binding и state до callback, допускает один exchange и проверяет возврат stored family и CSRF completed replay без tokens и второго provider call. Storage и provider — local fixtures, а не browser и IdP deployment.
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

// TestLoginUncertainOutcomeIsNotReplayedAfterRestart returns an uncertain callback result, reconstructs Logins over the same memory store and requires rejection without another exchange. It simulates restart semantics but proves no external store durability.
// TestLoginUncertainOutcomeIsNotReplayedAfterRestart задаёт uncertain callback result, пересоздаёт Logins над тем же memory store и требует отказа без нового exchange. Моделируется restart semantics без доказательства external store durability.
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
