// Package oidcprincipal composes standard signature verification with the durable owner; it never consumes custom JWT authority claims.
// Package oidcprincipal объединяет standard signature verification с durable owner; custom JWT authority claims не используются.
package oidcprincipal

import (
	"context"
	"slices"
	"time"

	"github.com/codex-k8s/kodex/libs/go/oidcverifier"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/google/uuid"
)

// IdentityVerifier verifies the real token's signature, issuer, audience, expiry and standard identity evidence before owner access.
// IdentityVerifier проверяет подпись реального token, issuer, audience, expiry и standard identity evidence до обращения к owner.
type IdentityVerifier interface {
	VerifyIdentity(context.Context, string) (oidcverifier.VerifiedIdentity, error)
}

// Resolve authenticates standard evidence, passes only transport-derived caller selectors to the owner and bounds the returned principal by both expiries.
// No JWT owner claim, organization field, revision fallback or arbitrary first-user rule participates in this projection.
// Resolve проверяет standard evidence, передаёт owner лишь transport-derived caller selectors и ограничивает principal обоими expiries.
// Проекция не использует JWT owner claim, organization field, revision fallback и правило произвольного первого пользователя.
func Resolve(ctx context.Context, verifier IdentityVerifier, owner platformrepo.StandardOIDCIdentityOwner, caller platformrepo.ProofPrincipalInput, token string) (oidcverifier.Principal, error) {
	if verifier == nil || owner == nil {
		return oidcverifier.Principal{}, errs.ErrUnavailable
	}
	identity, err := verifier.VerifyIdentity(ctx, token)
	if err != nil {
		return oidcverifier.Principal{}, err
	}
	binding, err := owner.ResolveStandardOIDCIdentity(ctx, platformrepo.StandardOIDCIdentity{
		CallerWorkload: caller.CallerWorkload, Operation: caller.Operation, RPCProfile: caller.RPCProfile,
		Issuer: identity.Issuer, Subject: identity.Subject, SessionID: identity.SessionID, TokenID: identity.TokenID,
		IssuedAt: identity.IssuedAt, AuthenticatedAt: identity.AuthenticatedAt, ExpiresAt: identity.ExpiresAt,
		DisplayName: identity.DisplayName, EmailHint: identity.EmailHint, Groups: slices.Clone(identity.Groups), ACR: identity.ACR, AMR: slices.Clone(identity.AMR),
	})
	if err != nil {
		return oidcverifier.Principal{}, err
	}
	organization, orgErr := uuid.Parse(binding.OrganizationID)
	actor, actorErr := uuid.Parse(binding.ActorID)
	closure := platformrepo.StandardOIDCClosureOperationAllowed(caller.Operation) && caller.CallerWorkload == "control-api-gateway" && caller.ProjectRef == ""
	if orgErr != nil || actorErr != nil || organization == uuid.Nil || actor == uuid.Nil || binding.Revision == 0 || binding.Revision > 1<<53-1 ||
		binding.Issuer != identity.Issuer || binding.Subject != identity.Subject || binding.SessionID != identity.SessionID ||
		!binding.AuthenticatedAt.Equal(identity.AuthenticatedAt) ||
		!closure && (binding.State != "ACTIVE" || !binding.AbsoluteExpiry.After(time.Now())) ||
		closure && binding.State != "ACTIVE" && binding.State != "REVOKED" {
		return oidcverifier.Principal{}, errs.ErrForbidden
	}
	expiresAt := identity.ExpiresAt
	if !closure && binding.AbsoluteExpiry.Before(expiresAt) {
		expiresAt = binding.AbsoluteExpiry
	}
	return oidcverifier.Principal{Subject: identity.Subject, OrganizationID: binding.OrganizationID, Issuer: identity.Issuer,
		SessionID: identity.SessionID, SessionRevision: binding.Revision, PlatformManagedOIDC: true, IssuedAt: identity.IssuedAt, PlatformSessionExpiresAt: binding.AbsoluteExpiry,
		AuthenticatedAt: identity.AuthenticatedAt, ExpiresAt: expiresAt, DisplayName: identity.DisplayName, EmailHint: identity.EmailHint,
		Groups: slices.Clone(identity.Groups), ACR: identity.ACR, AMR: slices.Clone(identity.AMR)}, nil
}
