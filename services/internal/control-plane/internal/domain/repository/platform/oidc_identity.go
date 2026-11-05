package platform

import (
	"context"
	"time"
)

const (
	// StandardOIDCResolveOperation resolves current application eligibility and may bootstrap only the explicitly mapped owner.
	// StandardOIDCResolveOperation разрешает текущую application eligibility и допускает bootstrap лишь явно сопоставленного owner.
	StandardOIDCResolveOperation = "platform.oidc.session.resolve"
	// StandardOIDCClosureOperation reads only the verified caller's existing session tombstone without ordinary application authority.
	// StandardOIDCClosureOperation читает лишь существующий session tombstone проверенного caller без обычной application authority.
	StandardOIDCClosureOperation = "platform.oidc.session.closure.get"
	// StandardOIDCRevokeOperation closes only the verified caller's existing session and permits idempotent tombstone reconciliation.
	// StandardOIDCRevokeOperation закрывает лишь существующую сессию проверенного caller и допускает идемпотентную сверку tombstone.
	StandardOIDCRevokeOperation = "platform.oidc.session.revoke"
)

// StandardOIDCClosureOperationAllowed classifies the two exact self-lifecycle operations; it grants no trust to an unverified request selector.
// StandardOIDCClosureOperationAllowed классифицирует две точные self-lifecycle operations, не доверяя непроверенному request selector.
func StandardOIDCClosureOperationAllowed(operation string) bool {
	return operation == StandardOIDCClosureOperation || operation == StandardOIDCRevokeOperation
}

// StandardOIDCSessionReader reads the exact tuple supplied by a verified transport principal, never arbitrary browser selectors.
// StandardOIDCSessionReader читает точный tuple проверенного transport principal, а не произвольные browser selectors.
type StandardOIDCSessionReader interface {
	ReadStandardOIDCSession(context.Context, string, string, uint64, bool) (StandardOIDCBinding, error)
}

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
