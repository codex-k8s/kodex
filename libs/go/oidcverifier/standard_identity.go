package oidcverifier

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/oidcidentity"
	coreoidc "github.com/coreos/go-oidc/v3/oidc"
)

// invalidIdentityClaimsMessage classifies invalid standard evidence without serializing a token or parser error.
// invalidIdentityClaimsMessage обозначает неверные standard evidence без сериализации token и parser error.
const invalidIdentityClaimsMessage = "OIDC identity claims are invalid"

// invalidBearerMessage preserves one safe bearer diagnostic without returning raw verification errors.
// invalidBearerMessage сохраняет единый безопасный bearer diagnostic без возврата сырых verification errors.
const invalidBearerMessage = "OIDC bearer is invalid"

// invalidSessionClaimsMessage preserves the legacy session-evidence diagnostic across both projections.
// invalidSessionClaimsMessage сохраняет legacy diagnostic для session evidence в обеих проекциях.
const invalidSessionClaimsMessage = "OIDC session claims are invalid"

// VerifiedIdentity contains only checked issuer-scoped identity and bounded authentication evidence.
// It contains no organization, application revision or owner flag and cannot replace an authorized Principal.
// VerifiedIdentity содержит только проверенную issuer-scoped identity и ограниченные authentication evidence.
// В нём отсутствуют организация, прикладная revision и owner flag; он не заменяет авторизованный Principal.
type VerifiedIdentity struct {
	Subject         string
	Issuer          string
	SessionID       string
	TokenID         string
	DisplayName     string
	EmailHint       string
	Groups          []string
	IssuedAt        time.Time
	AuthenticatedAt time.Time
	ACR             string
	AMR             []string
	ExpiresAt       time.Time
}

// standardClaims reads identity evidence independently of the stricter legacy application-authority shape.
// Unknown custom claims, even malformed organization/revision/owner fields, never supply authority through this shape.
// standardClaims читает identity evidence независимо от более строгого legacy-типа прикладных authority claims.
// Неизвестные custom claims, включая неверные organization/revision/owner fields, не передают authority через этот тип.
type standardClaims struct {
	SessionID     string   `json:"sid"`
	TokenID       string   `json:"jti"`
	Name          string   `json:"name"`
	PreferredName string   `json:"preferred_username"`
	Email         string   `json:"email"`
	EmailVerified bool     `json:"email_verified"`
	Groups        []string `json:"groups"`
	AuthTime      int64    `json:"auth_time"`
	ACR           string   `json:"acr"`
	AMR           []string `json:"amr"`
}

// verifiedToken preserves signature, issuer, audience, expiry and signing-key degradation checks for both verification methods.
// It returns no unchecked JWT payload and rejects incomplete verifier construction.
// verifiedToken сохраняет проверки подписи, issuer, audience, expiry и деградации signing keys для обоих методов.
// Непроверенный JWT payload не возвращается; неполный verifier означает закрытый отказ.
func (verifier *Verifier) verifiedToken(ctx context.Context, raw string) (*coreoidc.IDToken, error) {
	if verifier == nil || verifier.keys == nil || verifier.verifier == nil || ctx == nil || raw == "" ||
		len(raw) > maximumBearerBytes || strings.TrimSpace(raw) != raw {
		return nil, errors.New(invalidBearerMessage)
	}
	if err := verifier.keys.verificationError(); err != nil {
		return nil, err
	}
	token, err := verifier.verifier.Verify(ctx, raw)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, err
	}
	if err != nil {
		if availabilityErr := verifier.keys.verificationError(); availabilityErr != nil {
			return nil, availabilityErr
		}
		return nil, errors.New(invalidBearerMessage)
	}
	if token.Expiry.IsZero() {
		return nil, errors.New(invalidBearerMessage)
	}
	if deadline, degraded := verifier.keys.DegradedDeadline(); degraded && token.Expiry.After(deadline) {
		return nil, errors.New("OIDC bearer exceeds the signing-key grace window")
	}
	return token, nil
}

// standardIdentityFromToken projects a verified token after its caller has selected and validated the exact subject normalization.
// Existing session/group/authentication checks remain; no platform session, tenant or permissions are allocated here.
// standardIdentityFromToken проецирует проверенный token после выбора и проверки точной нормализации subject вызывающей стороной.
// Сохраняет session/group/authentication checks; platform session, tenant и permissions здесь не создаются.
func standardIdentityFromToken(token *coreoidc.IDToken, subject string) (VerifiedIdentity, error) {
	var values standardClaims
	if token.Claims(&values) != nil {
		return VerifiedIdentity{}, errors.New(invalidSessionClaimsMessage)
	}
	groups, groupsErr := normalizeGroups(values.Groups)
	amr, amrErr := normalizeBoundedStrings(values.AMR, maximumAMRValues, maximumAMRRunes)
	acr := strings.TrimSpace(values.ACR)
	if groupsErr != nil || amrErr != nil || utf8.RuneCountInString(acr) > maximumACRRunes || strings.ContainsAny(acr, "\r\n\x00") ||
		values.AuthTime < 0 || values.AuthTime > token.Expiry.Unix() {
		return VerifiedIdentity{}, errors.New(invalidSessionClaimsMessage)
	}
	sessionID, sessionErr := oidcidentity.SessionID(token.Issuer, values.SessionID)
	tokenID, tokenIDErr := oidcidentity.TokenID(token.Issuer, values.TokenID)
	if sessionErr != nil || tokenIDErr != nil {
		return VerifiedIdentity{}, errors.New(invalidSessionClaimsMessage)
	}
	var authenticatedAt time.Time
	if values.AuthTime > 0 {
		authenticatedAt = time.Unix(values.AuthTime, 0).UTC()
	}
	return VerifiedIdentity{
		Subject: subject, Issuer: token.Issuer, SessionID: sessionID, TokenID: tokenID,
		DisplayName: safeDisplayName(values.Name, values.PreferredName), EmailHint: maskedVerifiedEmail(values.Email, values.EmailVerified),
		Groups: groups, IssuedAt: token.IssuedAt, AuthenticatedAt: authenticatedAt, ACR: acr, AMR: amr, ExpiresAt: token.Expiry,
	}, nil
}

// VerifyIdentity verifies standard identity without application-authority claims and requires signed issuance-time evidence.
// Callers must resolve current platform bindings and session state before this identity can authorize an operation.
// VerifyIdentity проверяет standard identity без прикладных authority claims и требует подписанное время выпуска.
// До авторизации операции caller обязан разрешить текущие platform bindings и состояние сессии.
func (verifier *Verifier) VerifyIdentity(ctx context.Context, raw string) (VerifiedIdentity, error) {
	token, err := verifier.verifiedToken(ctx, raw)
	if err != nil {
		return VerifiedIdentity{}, err
	}
	subject, err := oidcidentity.ScopedSubject(token.Issuer, token.Subject)
	if err != nil {
		return VerifiedIdentity{}, errors.New(invalidIdentityClaimsMessage)
	}
	identity, err := standardIdentityFromToken(token, subject)
	if err != nil {
		return VerifiedIdentity{}, err
	}
	if identity.IssuedAt.IsZero() || identity.IssuedAt.After(identity.ExpiresAt) || identity.IssuedAt.After(time.Now().Add(30*time.Second)) {
		return VerifiedIdentity{}, errors.New(invalidIdentityClaimsMessage)
	}
	return identity, nil
}
