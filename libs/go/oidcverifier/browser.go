package oidcverifier

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/oidcidentity"
	coreoidc "github.com/coreos/go-oidc/v3/oidc"
)

var (
	// ErrGrantRejected marks an explicit invalid grant returned by the token endpoint or local input validation.
	// ErrGrantRejected обозначает явный отказ в grant от token endpoint или локальной проверки входа.
	ErrGrantRejected = errors.New("OIDC grant was rejected")
	// ErrExchangeUncertain marks an exchange whose provider outcome cannot be safely inferred or retried.
	// ErrExchangeUncertain обозначает обмен с неизвестным итогом провайдера, который нельзя безопасно повторять.
	ErrExchangeUncertain = errors.New("OIDC exchange outcome is uncertain")
)

// BrowserClient exchanges browser grants using the bearer's exact TLS transport and signing keys without automatic retries.
// BrowserClient обменивает браузерные grants через точный TLS transport и ключи bearer; автоматических повторов нет.
type BrowserClient struct {
	verifier    *Verifier
	idVerifier  *coreoidc.IDTokenVerifier
	clientID    string
	redirectURI string
	tokenURL    string
}

// BrowserTokens contains verified access and rotated refresh tokens plus the bounded refresh expiry and principal.
// BrowserTokens содержит проверенный access token, обновлённый refresh token, ограниченный срок его действия и principal.
type BrowserTokens struct {
	AccessToken      string
	RefreshToken     string
	RefreshExpiresAt time.Time
	Principal        Principal
}

// BrowserClient validates the redirect and audience, then binds an ID token verifier to this verifier's issuer and keys.
// BrowserClient проверяет redirect и audience, затем связывает проверку ID token с issuer и ключами данного verifier.
func (verifier *Verifier) BrowserClient(clientID, redirectURI string) (*BrowserClient, error) {
	redirect, err := url.Parse(redirectURI)
	if verifier == nil || verifier.client == nil || verifier.keys == nil || verifier.issuer == "" ||
		clientID == "" || len(clientID) > 200 || strings.ContainsAny(clientID, " \r\n\t") ||
		err != nil || redirect.Scheme != "https" || redirect.Hostname() == "" || redirect.User != nil ||
		redirect.RawQuery != "" || redirect.Fragment != "" || redirect.Path == "" {
		return nil, errors.New("OIDC browser client configuration is invalid")
	}
	return &BrowserClient{verifier: verifier, clientID: clientID, redirectURI: redirectURI,
		tokenURL:   strings.TrimRight(verifier.issuer, "/") + "/protocol/openid-connect/token",
		idVerifier: coreoidc.NewVerifier(verifier.issuer, verifier.keys, &coreoidc.Config{ClientID: clientID, SupportedSigningAlgs: []string{"RS256"}})}, nil
}

// AuthorizationURL builds a PKCE S256 authorization request; fresh login additionally forces reauthentication.
// AuthorizationURL формирует запрос авторизации с PKCE S256; fresh дополнительно требует повторного входа.
func (client *BrowserClient) AuthorizationURL(state, nonce, challenge string, fresh bool) (string, error) {
	if client == nil || !boundedOpaque(state, 43, 128) || !boundedOpaque(nonce, 43, 128) || !boundedOpaque(challenge, 43, 43) {
		return "", errors.New("OIDC authorization input is invalid")
	}
	query := url.Values{"client_id": {client.clientID}, "redirect_uri": {client.redirectURI}, "response_type": {"code"},
		"scope": {"openid kodex.owner"}, "state": {state}, "nonce": {nonce}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}}
	if fresh {
		query.Set("prompt", "login")
		query.Set("max_age", "0")
	}
	return strings.TrimRight(client.verifier.issuer, "/") + "/protocol/openid-connect/auth?" + query.Encode(), nil
}

// ExchangeCode submits one authorization code with its redirect and verifier, then checks access and ID token identity.
// ExchangeCode однократно отправляет authorization code с redirect и verifier, затем сверяет access и ID token.
func (client *BrowserClient) ExchangeCode(ctx context.Context, code, verifier, nonce string) (BrowserTokens, error) {
	if client == nil || !boundedOpaque(code, 1, 4096) || !boundedOpaque(verifier, 43, 128) || !boundedOpaque(nonce, 43, 128) {
		return BrowserTokens{}, ErrGrantRejected
	}
	return client.exchange(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {client.redirectURI}, "code_verifier": {verifier}}, nonce)
}

// Refresh submits one refresh grant and accepts only a verified replacement access token and refresh token.
// Refresh однократно отправляет refresh grant и принимает только проверенные новые access и refresh token.
func (client *BrowserClient) Refresh(ctx context.Context, refreshToken string) (BrowserTokens, error) {
	if client == nil || !boundedOpaque(refreshToken, 1, maximumBearerBytes) {
		return BrowserTokens{}, ErrGrantRejected
	}
	return client.exchange(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}}, "")
}

// exchange bounds the provider response and distinguishes explicit invalid_grant from every uncertain failure.
// exchange ограничивает ответ провайдера и отличает явный invalid_grant от любого неопределённого отказа.
func (client *BrowserClient) exchange(ctx context.Context, form url.Values, nonce string) (BrowserTokens, error) {
	form.Set("client_id", client.clientID)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return BrowserTokens{}, ErrExchangeUncertain
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := client.verifier.client.Do(request)
	if err != nil {
		return BrowserTokens{}, ErrExchangeUncertain
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 64<<10+1))
	if err != nil || len(raw) > 64<<10 {
		return BrowserTokens{}, ErrExchangeUncertain
	}
	if response.StatusCode != http.StatusOK {
		var failure struct {
			Error string `json:"error"`
		}
		if response.StatusCode == http.StatusBadRequest && json.Unmarshal(raw, &failure) == nil && failure.Error == "invalid_grant" {
			return BrowserTokens{}, ErrGrantRejected
		}
		return BrowserTokens{}, ErrExchangeUncertain
	}
	var result struct {
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		IDToken          string `json:"id_token"`
		TokenType        string `json:"token_type"`
		ExpiresIn        int64  `json:"expires_in"`
		RefreshExpiresIn int64  `json:"refresh_expires_in"`
	}
	if json.Unmarshal(raw, &result) != nil || !strings.EqualFold(result.TokenType, "Bearer") ||
		!boundedOpaque(result.AccessToken, 1, maximumBearerBytes) || !boundedOpaque(result.RefreshToken, 1, maximumBearerBytes) ||
		result.ExpiresIn <= 0 || result.RefreshExpiresIn <= 0 || result.RefreshExpiresIn > 12*60*60 {
		return BrowserTokens{}, ErrExchangeUncertain
	}
	principal, err := client.verifier.VerifyToken(ctx, result.AccessToken)
	if err != nil || principal.AuthenticatedAt.IsZero() {
		return BrowserTokens{}, ErrExchangeUncertain
	}
	if nonce != "" {
		if err := client.verifyLogin(ctx, result.IDToken, result.AccessToken, nonce, principal); err != nil {
			return BrowserTokens{}, ErrExchangeUncertain
		}
	}
	return BrowserTokens{AccessToken: result.AccessToken, RefreshToken: result.RefreshToken,
		RefreshExpiresAt: time.Now().Add(time.Duration(result.RefreshExpiresIn) * time.Second), Principal: principal}, nil
}

// verifyLogin requires the ID token nonce, issuer-scoped subject/session, revision and auth time to match the access token.
// verifyLogin требует совпадения nonce, привязанных к issuer subject/session, revision и времени входа с access token.
func (client *BrowserClient) verifyLogin(ctx context.Context, raw, access, nonce string, principal Principal) error {
	if !boundedOpaque(raw, 1, maximumBearerBytes) {
		return ErrGrantRejected
	}
	id, err := client.idVerifier.Verify(ctx, raw)
	if err != nil || subtle.ConstantTimeCompare([]byte(id.Nonce), []byte(nonce)) != 1 {
		return ErrGrantRejected
	}
	if id.AccessTokenHash != "" && id.VerifyAccessToken(access) != nil {
		return ErrGrantRejected
	}
	var values claims
	if id.Claims(&values) != nil {
		return ErrGrantRejected
	}
	subject, subjectErr := oidcidentity.Subject(id.Issuer, id.Subject)
	sessionID, sessionErr := oidcidentity.SessionID(id.Issuer, values.SessionID)
	if subjectErr != nil || sessionErr != nil || subject != principal.Subject || sessionID != principal.SessionID ||
		values.OrganizationID != principal.OrganizationID || values.SessionRevision != principal.SessionRevision ||
		values.AuthTime != principal.AuthenticatedAt.Unix() {
		return ErrGrantRejected
	}
	return nil
}

// boundedOpaque accepts only printable ASCII without whitespace within the supplied byte-length limits.
// boundedOpaque принимает только печатные ASCII-байты без пробелов в заданных пределах длины.
func boundedOpaque(value string, minimum, maximum int) bool {
	if len(value) < minimum || len(value) > maximum {
		return false
	}
	for _, r := range value {
		if r < 33 || r > 126 {
			return false
		}
	}
	return true
}
