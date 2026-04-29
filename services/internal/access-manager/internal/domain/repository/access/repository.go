// Package access defines persistence ports owned by the access domain service.
package access

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/query"
)

// Repository is the domain persistence contract for access-manager use cases.
type Repository interface {
	// CreateOrganization stores an organization and its outbox event atomically.
	CreateOrganization(ctx context.Context, organization entity.Organization, event entity.OutboxEvent) error
	// GetOrganization returns an organization by id.
	GetOrganization(ctx context.Context, id uuid.UUID) (entity.Organization, error)
	// CountActiveOwnerOrganizations returns active owner organization count.
	CountActiveOwnerOrganizations(ctx context.Context) (int, error)
	// CreateUser stores a user, first identity and outbox event atomically.
	CreateUser(ctx context.Context, user entity.User, identity entity.UserIdentity, event entity.OutboxEvent) error
	// GetUser returns a user by id.
	GetUser(ctx context.Context, id uuid.UUID) (entity.User, error)
	// GetUserByEmail returns a user by normalized primary email.
	GetUserByEmail(ctx context.Context, email string) (entity.User, error)
	// GetUserByIdentity returns a user linked to provider subject.
	GetUserByIdentity(ctx context.Context, provider enum.IdentityProvider, subject string) (entity.User, error)
	// LinkUserIdentity stores an additional identity link and outbox event atomically.
	LinkUserIdentity(ctx context.Context, identity entity.UserIdentity, event entity.OutboxEvent) error
	// PutAllowlistEntry upserts an allowlist entry and its outbox event.
	PutAllowlistEntry(ctx context.Context, entry entity.AllowlistEntry, event entity.OutboxEvent) error
	// FindAllowlistEntry returns an allowlist entry by match key.
	FindAllowlistEntry(ctx context.Context, matchType enum.AllowlistMatchType, value string) (entity.AllowlistEntry, error)
	// CreateGroup stores a group and its outbox event atomically.
	CreateGroup(ctx context.Context, group entity.Group, event entity.OutboxEvent) error
	// GetGroup returns a group by id.
	GetGroup(ctx context.Context, id uuid.UUID) (entity.Group, error)
	// FindMembership returns a membership by natural identity.
	FindMembership(ctx context.Context, identity query.MembershipIdentity) (entity.Membership, error)
	// SetMembership upserts a membership and its outbox event atomically.
	SetMembership(ctx context.Context, membership entity.Membership, event entity.OutboxEvent) error
	// ListMemberships returns memberships for graph expansion.
	ListMemberships(ctx context.Context, filter query.MembershipGraphFilter) ([]entity.Membership, error)
	// PutExternalProvider upserts an external provider and its outbox event.
	PutExternalProvider(ctx context.Context, provider entity.ExternalProvider, event entity.OutboxEvent) error
	// GetExternalProvider returns an external provider by id.
	GetExternalProvider(ctx context.Context, id uuid.UUID) (entity.ExternalProvider, error)
	// GetExternalProviderBySlug returns an external provider by stable slug.
	GetExternalProviderBySlug(ctx context.Context, slug string) (entity.ExternalProvider, error)
	// RegisterExternalAccount stores an external account and its outbox event.
	RegisterExternalAccount(ctx context.Context, account entity.ExternalAccount, event entity.OutboxEvent) error
	// GetExternalAccount returns an external account by id.
	GetExternalAccount(ctx context.Context, id uuid.UUID) (entity.ExternalAccount, error)
	// BindExternalAccount stores an account binding and its outbox event.
	BindExternalAccount(ctx context.Context, binding entity.ExternalAccountBinding, event entity.OutboxEvent) error
	// FindExternalAccountBinding returns a binding for requested account usage.
	FindExternalAccountBinding(ctx context.Context, filter query.ExternalAccountUsageFilter) (entity.ExternalAccountBinding, error)
	// FindExternalAccountBindingByIdentity returns a binding by account and usage scope.
	FindExternalAccountBindingByIdentity(ctx context.Context, identity query.ExternalAccountBindingIdentity) (entity.ExternalAccountBinding, error)
	// PutSecretBindingRef upserts a secret reference and its outbox event.
	PutSecretBindingRef(ctx context.Context, secret entity.SecretBindingRef, event entity.OutboxEvent) error
	// GetSecretBindingRef returns a secret reference by id.
	GetSecretBindingRef(ctx context.Context, id uuid.UUID) (entity.SecretBindingRef, error)
	// PutAccessAction upserts an access action and its outbox event.
	PutAccessAction(ctx context.Context, action entity.AccessAction, event entity.OutboxEvent) error
	// GetAccessActionByKey returns an action by canonical key.
	GetAccessActionByKey(ctx context.Context, key string) (entity.AccessAction, error)
	// PutAccessRule upserts a policy rule and its outbox event.
	PutAccessRule(ctx context.Context, rule entity.AccessRule, event entity.OutboxEvent) error
	// FindAccessRule returns a policy rule by stable business identity.
	FindAccessRule(ctx context.Context, identity query.AccessRuleIdentity) (entity.AccessRule, error)
	// ListAccessRules returns policy rules applicable to a check.
	ListAccessRules(ctx context.Context, filter query.AccessRuleFilter) ([]entity.AccessRule, error)
	// RecordAccessDecision stores an audit record and optional outbox event.
	RecordAccessDecision(ctx context.Context, audit entity.AccessDecisionAudit, event *entity.OutboxEvent) error
}

// Clock provides deterministic time for domain commands and tests.
type Clock interface {
	Now() time.Time
}

// IDGenerator provides aggregate and event identifiers for domain commands.
type IDGenerator interface {
	New() uuid.UUID
}
