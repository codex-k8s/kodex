package platform

import (
	"context"
	"time"
)

// StandardOIDCIdentityOwner is the narrow durable identity/session port used only by the explicitly selected standard-identity adapter.
// StandardOIDCIdentityOwner — узкий durable identity/session port для явно выбранного standard-identity adapter.
type StandardOIDCIdentityOwner interface {
	ResolveStandardOIDCIdentity(context.Context, StandardOIDCIdentity) (StandardOIDCBinding, error)
	RevokeStandardOIDCSession(context.Context, string, string, uint64) error
}

// StandardOIDCIdentity carries signature-verified identity evidence from the native authentication adapter, never browser authority fields.
// Subject is issuer-qualified; policy, organization, application revision and bootstrap ownership are assigned by the repository owner.
// StandardOIDCIdentity содержит signature-verified identity evidence native authentication adapter, а не browser authority fields.
// Subject привязан к issuer; policy, организация, application revision и bootstrap ownership назначаются владельцем repository.
type StandardOIDCIdentity struct {
	CallerWorkload  string
	Operation       string
	RPCProfile      string
	Issuer          string
	Subject         string
	SessionID       string
	TokenID         string
	IssuedAt        time.Time
	AuthenticatedAt time.Time
	ExpiresAt       time.Time
	DisplayName     string
	EmailHint       string
	Groups          []string
	ACR             string
	AMR             []string
}

// StandardOIDCBinding is the durable owner-selected installation/actor/session binding after eligibility checks.
// Revision comes from database-owned allocation; it is not a custom JWT claim or a constant fallback.
// StandardOIDCBinding — устойчивый owner-selected binding установки/actor/session после eligibility checks.
// Revision выделяет база данных; это не custom JWT claim и не константный fallback.
type StandardOIDCBinding struct {
	OrganizationID  string
	ActorID         string
	Issuer          string
	Subject         string
	SessionID       string
	Revision        uint64
	PolicyRevision  uint64
	PolicyDigest    string
	AuthenticatedAt time.Time
	AbsoluteExpiry  time.Time
	LastIssuedAt    time.Time
	State           string
}
