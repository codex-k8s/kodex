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
	ErrGrantRejected     = errors.New("OIDC grant was rejected")
	ErrExchangeUncertain = errors.New("OIDC exchange outcome is uncertain")
)

// BrowserClient использует тот же exact TLS transport и JWKS, что bearer verifier.
// Вызовы не повторяются: внешний token endpoint не имеет idempotency contract.
type BrowserClient struct {
	verifier    *Verifier
	idVerifier  *coreoidc.IDTokenVerifier
	clientID    string
	redirectURI string
	tokenURL    string
}

type BrowserTokens struct {
	AccessToken      string
	RefreshToken     string
	RefreshExpiresAt time.Time
	Principal        Principal
}

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

func (client *BrowserClient) ExchangeCode(ctx context.Context, code, verifier, nonce string) (BrowserTokens, error) {
	if client == nil || !boundedOpaque(code, 1, 4096) || !boundedOpaque(verifier, 43, 128) || !boundedOpaque(nonce, 43, 128) {
		return BrowserTokens{}, ErrGrantRejected
	}
	return client.exchange(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {client.redirectURI}, "code_verifier": {verifier}}, nonce)
}

func (client *BrowserClient) Refresh(ctx context.Context, refreshToken string) (BrowserTokens, error) {
	if client == nil || !boundedOpaque(refreshToken, 1, maximumBearerBytes) {
		return BrowserTokens{}, ErrGrantRejected
	}
	return client.exchange(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}}, "")
}

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
