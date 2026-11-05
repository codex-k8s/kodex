package oidcverifier

import (
	"context"
	"crypto/subtle"
	"net/url"
	"time"

	"github.com/codex-k8s/kodex/libs/go/oidcidentity"
)

// browserTokenResponse is the bounded token-endpoint payload before identity verification; values must never be logged or serialized to clients.
// browserTokenResponse — ограниченный payload token endpoint до проверки identity; значения нельзя логировать или выдавать клиентам.
type browserTokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	IDToken          string `json:"id_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int64  `json:"expires_in"`
	RefreshExpiresIn int64  `json:"refresh_expires_in"`
}

// IdentityBrowserTokens carries verified standard identity and server-only tokens without constructing application authority.
// An owner adapter must resolve platform session/permissions before these values can enter an authorized browser family.
// IdentityBrowserTokens содержит проверенную standard identity и server-only tokens без создания application authority.
// Owner adapter обязан разрешить platform session/permissions до включения этих значений в авторизованное browser family.
type IdentityBrowserTokens struct {
	AccessToken      string
	RefreshToken     string
	RefreshExpiresAt time.Time
	Identity         VerifiedIdentity
}

// ExchangeIdentityCode performs one PKCE-bound code exchange and verifies both signed tokens and nonce without consuming custom authority claims.
// It does not mint a session or bypass the platform owner resolver.
// ExchangeIdentityCode однократно обменивает PKCE-bound code, проверяет обе подписи и nonce без custom authority claims.
// Метод не создаёт сессию и не обходит platform owner resolver.
func (client *BrowserClient) ExchangeIdentityCode(ctx context.Context, code, verifier, nonce string) (IdentityBrowserTokens, error) {
	if client == nil || !boundedOpaque(code, 1, 4096) || !boundedOpaque(verifier, 43, 128) || !boundedOpaque(nonce, 43, 128) {
		return IdentityBrowserTokens{}, ErrGrantRejected
	}
	return client.exchangeIdentity(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {client.redirectURI}, "code_verifier": {verifier}}, nonce)
}

// RefreshIdentity submits one refresh grant and returns checked standard evidence; the caller must compare its previous identity and durable family fence.
// This method never retries an uncertain rotated refresh token or invents an application revision.
// RefreshIdentity однократно отправляет refresh grant; caller обязан сверить прежнюю identity и durable family fence.
// Метод не повторяет uncertain rotated refresh token и не выдумывает application revision.
func (client *BrowserClient) RefreshIdentity(ctx context.Context, refreshToken string) (IdentityBrowserTokens, error) {
	if client == nil || !boundedOpaque(refreshToken, 1, maximumBearerBytes) {
		return IdentityBrowserTokens{}, ErrGrantRejected
	}
	return client.exchangeIdentity(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}}, "")
}

// exchangeIdentity wraps the shared single-attempt transport with standard access-token and optional login ID-token checks.
// The returned type deliberately has no organization, owner flag or application revision.
// exchangeIdentity дополняет общий single-attempt transport проверками standard access token и login ID token.
// В возвращаемом типе намеренно отсутствуют организация, owner flag и application revision.
func (client *BrowserClient) exchangeIdentity(ctx context.Context, form url.Values, nonce string) (IdentityBrowserTokens, error) {
	result, err := client.exchangeResponse(ctx, form)
	if err != nil {
		return IdentityBrowserTokens{}, err
	}
	identity, err := client.verifier.VerifyIdentity(ctx, result.AccessToken)
	if err != nil || identity.AuthenticatedAt.IsZero() {
		return IdentityBrowserTokens{}, ErrExchangeUncertain
	}
	if nonce != "" {
		if err := client.verifyIdentityLogin(ctx, result.IDToken, result.AccessToken, nonce, identity); err != nil {
			return IdentityBrowserTokens{}, ErrExchangeUncertain
		}
	}
	return IdentityBrowserTokens{AccessToken: result.AccessToken, RefreshToken: result.RefreshToken,
		RefreshExpiresAt: time.Now().Add(time.Duration(result.RefreshExpiresIn) * time.Second), Identity: identity}, nil
}

// verifyIdentityLogin validates ID signature/audience, nonce, optional access-token hash and exact issuer-scoped subject/session/auth-time bindings.
// Custom organization/revision/owner claims are ignored because they are not application authority in this flow.
// verifyIdentityLogin проверяет ID signature/audience, nonce, optional access-token hash и точные issuer-scoped subject/session/auth-time bindings.
// Custom organization/revision/owner claims игнорируются, поскольку не задают application authority в этом flow.
func (client *BrowserClient) verifyIdentityLogin(ctx context.Context, raw, access, nonce string, identity VerifiedIdentity) error {
	if !boundedOpaque(raw, 1, maximumBearerBytes) || client.idVerifier == nil {
		return ErrGrantRejected
	}
	id, err := client.idVerifier.Verify(ctx, raw)
	if err != nil || subtle.ConstantTimeCompare([]byte(id.Nonce), []byte(nonce)) != 1 || id.IssuedAt.IsZero() ||
		id.IssuedAt.After(id.Expiry) || id.IssuedAt.After(time.Now().Add(30*time.Second)) {
		return ErrGrantRejected
	}
	if id.AccessTokenHash != "" && id.VerifyAccessToken(access) != nil {
		return ErrGrantRejected
	}
	var values standardClaims
	if id.Claims(&values) != nil {
		return ErrGrantRejected
	}
	subject, subjectErr := oidcidentity.ScopedSubject(id.Issuer, id.Subject)
	sessionID, sessionErr := oidcidentity.SessionID(id.Issuer, values.SessionID)
	if subjectErr != nil || sessionErr != nil || subject != identity.Subject || sessionID != identity.SessionID ||
		id.Issuer != identity.Issuer || values.AuthTime != identity.AuthenticatedAt.Unix() {
		return ErrGrantRejected
	}
	return nil
}
