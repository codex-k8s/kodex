package access

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/query"
)

type Repository interface {
	CreateOrganization(ctx context.Context, organization entity.Organization, event entity.OutboxEvent) error
	GetOrganization(ctx context.Context, id uuid.UUID) (entity.Organization, error)
	CountActiveOwnerOrganizations(ctx context.Context) (int, error)
	CreateUser(ctx context.Context, user entity.User, identity entity.UserIdentity, event entity.OutboxEvent) error
	GetUser(ctx context.Context, id uuid.UUID) (entity.User, error)
	GetUserByIdentity(ctx context.Context, provider enum.IdentityProvider, subject string) (entity.User, error)
	LinkUserIdentity(ctx context.Context, identity entity.UserIdentity, event entity.OutboxEvent) error
	PutAllowlistEntry(ctx context.Context, entry entity.AllowlistEntry, event entity.OutboxEvent) error
	FindAllowlistEntry(ctx context.Context, matchType enum.AllowlistMatchType, value string) (entity.AllowlistEntry, error)
	CreateGroup(ctx context.Context, group entity.Group, event entity.OutboxEvent) error
	GetGroup(ctx context.Context, id uuid.UUID) (entity.Group, error)
	FindMembership(ctx context.Context, identity query.MembershipIdentity) (entity.Membership, error)
	SetMembership(ctx context.Context, membership entity.Membership, event entity.OutboxEvent) error
	ListMemberships(ctx context.Context, filter query.MembershipGraphFilter) ([]entity.Membership, error)
	PutExternalProvider(ctx context.Context, provider entity.ExternalProvider, event entity.OutboxEvent) error
	GetExternalProvider(ctx context.Context, id uuid.UUID) (entity.ExternalProvider, error)
	RegisterExternalAccount(ctx context.Context, account entity.ExternalAccount, event entity.OutboxEvent) error
	GetExternalAccount(ctx context.Context, id uuid.UUID) (entity.ExternalAccount, error)
	BindExternalAccount(ctx context.Context, binding entity.ExternalAccountBinding, event entity.OutboxEvent) error
	FindExternalAccountBinding(ctx context.Context, filter query.ExternalAccountUsageFilter) (entity.ExternalAccountBinding, error)
	PutSecretBindingRef(ctx context.Context, secret entity.SecretBindingRef, event entity.OutboxEvent) error
	GetSecretBindingRef(ctx context.Context, id uuid.UUID) (entity.SecretBindingRef, error)
	PutAccessAction(ctx context.Context, action entity.AccessAction, event entity.OutboxEvent) error
	GetAccessActionByKey(ctx context.Context, key string) (entity.AccessAction, error)
	PutAccessRule(ctx context.Context, rule entity.AccessRule, event entity.OutboxEvent) error
	ListAccessRules(ctx context.Context, filter query.AccessRuleFilter) ([]entity.AccessRule, error)
	RecordAccessDecision(ctx context.Context, audit entity.AccessDecisionAudit, event *entity.OutboxEvent) error
}

type Clock interface {
	Now() time.Time
}

type IDGenerator interface {
	New() uuid.UUID
}
