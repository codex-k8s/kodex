package access

import (
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/query"
)

func organizationArgs(organization entity.Organization) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                     organization.ID,
		"kind":                   string(organization.Kind),
		"slug":                   organization.Slug,
		"display_name":           organization.DisplayName,
		"image_asset_ref":        organization.ImageAssetRef,
		"status":                 string(organization.Status),
		"parent_organization_id": nullableUUID(organization.ParentOrganizationID),
		"version":                organization.Version,
		"created_at":             organization.CreatedAt,
		"updated_at":             organization.UpdatedAt,
	}
}

func userArgs(user entity.User) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":               user.ID,
		"primary_email":    user.PrimaryEmail,
		"display_name":     user.DisplayName,
		"avatar_asset_ref": user.AvatarAssetRef,
		"status":           string(user.Status),
		"locale":           user.Locale,
		"version":          user.Version,
		"created_at":       user.CreatedAt,
		"updated_at":       user.UpdatedAt,
	}
}

func userUpdateArgs(user entity.User, previousVersion int64) pgx.NamedArgs {
	args := userArgs(user)
	args["previous_version"] = previousVersion
	return args
}

func userIdentityArgs(identity entity.UserIdentity) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":             identity.ID,
		"user_id":        identity.UserID,
		"provider":       string(identity.Provider),
		"subject":        identity.Subject,
		"email_at_login": identity.EmailAtLogin,
		"last_login_at":  nullableTime(identity.LastLoginAt),
	}
}

func userIdentityLookupArgs(provider string, subject string) pgx.NamedArgs {
	return pgx.NamedArgs{"provider": provider, "subject": subject}
}

func commandIdentityArgs(identity query.CommandIdentity) pgx.NamedArgs {
	return pgx.NamedArgs{
		"command_id":      nullableCommandID(identity.CommandID),
		"idempotency_key": commandLookupIdempotencyKey(identity),
	}
}

func commandResultArgs(result entity.CommandResult) pgx.NamedArgs {
	return pgx.NamedArgs{
		"key":             result.Key,
		"command_id":      nullableCommandID(result.CommandID),
		"idempotency_key": result.IdempotencyKey,
		"operation":       result.Operation,
		"aggregate_type":  result.AggregateType,
		"aggregate_id":    result.AggregateID,
		"created_at":      result.CreatedAt,
	}
}

func allowlistEntryArgs(entry entity.AllowlistEntry) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":              entry.ID,
		"match_type":      string(entry.MatchType),
		"value":           entry.Value,
		"organization_id": nullableUUID(entry.OrganizationID),
		"default_status":  string(entry.DefaultStatus),
		"status":          string(entry.Status),
		"version":         entry.Version,
		"created_at":      entry.CreatedAt,
		"updated_at":      entry.UpdatedAt,
	}
}

func allowlistEntryUpdateArgs(entry entity.AllowlistEntry, previousVersion int64) pgx.NamedArgs {
	args := allowlistEntryArgs(entry)
	args["previous_version"] = previousVersion
	return args
}

func allowlistLookupArgs(matchType string, value string) pgx.NamedArgs {
	return pgx.NamedArgs{"match_type": matchType, "value": value}
}

func groupArgs(group entity.Group) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":              group.ID,
		"scope_type":      string(group.ScopeType),
		"scope_id":        nullableUUID(group.ScopeID),
		"slug":            group.Slug,
		"display_name":    group.DisplayName,
		"parent_group_id": nullableUUID(group.ParentGroupID),
		"image_asset_ref": group.ImageAssetRef,
		"status":          string(group.Status),
		"version":         group.Version,
		"created_at":      group.CreatedAt,
		"updated_at":      group.UpdatedAt,
	}
}

func membershipArgs(membership entity.Membership) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":           membership.ID,
		"subject_type": string(membership.SubjectType),
		"subject_id":   membership.SubjectID,
		"target_type":  string(membership.TargetType),
		"target_id":    membership.TargetID,
		"role_hint":    membership.RoleHint,
		"status":       string(membership.Status),
		"source":       string(membership.Source),
		"version":      membership.Version,
		"created_at":   membership.CreatedAt,
		"updated_at":   membership.UpdatedAt,
	}
}

func externalProviderArgs(provider entity.ExternalProvider) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":             provider.ID,
		"slug":           provider.Slug,
		"provider_kind":  string(provider.ProviderKind),
		"display_name":   provider.DisplayName,
		"icon_asset_ref": provider.IconAssetRef,
		"status":         string(provider.Status),
		"version":        provider.Version,
		"created_at":     provider.CreatedAt,
		"updated_at":     provider.UpdatedAt,
	}
}

func externalAccountArgs(account entity.ExternalAccount) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                    account.ID,
		"external_provider_id":  account.ExternalProviderID,
		"account_type":          string(account.AccountType),
		"display_name":          account.DisplayName,
		"image_asset_ref":       account.ImageAssetRef,
		"owner_scope_type":      string(account.OwnerScopeType),
		"owner_scope_id":        account.OwnerScopeID,
		"status":                string(account.Status),
		"secret_binding_ref_id": nullableUUID(account.SecretBindingRefID),
		"version":               account.Version,
		"created_at":            account.CreatedAt,
		"updated_at":            account.UpdatedAt,
	}
}

func externalAccountBindingArgs(binding entity.ExternalAccountBinding) pgx.NamedArgs {
	return withBaseArgs(binding.Base, pgx.NamedArgs{
		"external_account_id": binding.ExternalAccountID,
		"usage_scope_type":    string(binding.UsageScopeType),
		"usage_scope_id":      binding.UsageScopeID,
		"allowed_action_keys": binding.AllowedActionKeys,
		"status":              string(binding.Status),
	})
}

func secretBindingRefArgs(secret entity.SecretBindingRef) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                secret.ID,
		"store_type":        string(secret.StoreType),
		"store_ref":         secret.StoreRef,
		"value_fingerprint": secret.ValueFingerprint,
		"rotated_at":        nullableTime(secret.RotatedAt),
		"version":           secret.Version,
		"created_at":        secret.CreatedAt,
		"updated_at":        secret.UpdatedAt,
	}
}

func accessActionArgs(action entity.AccessAction) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":            action.ID,
		"key":           action.Key,
		"display_name":  action.DisplayName,
		"description":   action.Description,
		"resource_type": action.ResourceType,
		"status":        string(action.Status),
		"version":       action.Version,
		"created_at":    action.CreatedAt,
		"updated_at":    action.UpdatedAt,
	}
}

func accessRuleArgs(rule entity.AccessRule) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":            rule.ID,
		"effect":        string(rule.Effect),
		"subject_type":  string(rule.SubjectType),
		"subject_id":    rule.SubjectID,
		"action_key":    rule.ActionKey,
		"resource_type": rule.ResourceType,
		"resource_id":   rule.ResourceID,
		"scope_type":    rule.ScopeType,
		"scope_id":      rule.ScopeID,
		"priority":      rule.Priority,
		"status":        string(rule.Status),
		"version":       rule.Version,
		"created_at":    rule.CreatedAt,
		"updated_at":    rule.UpdatedAt,
	}
}

func accessDecisionAuditArgs(audit entity.AccessDecisionAudit, requestContext []byte, explanation []byte) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":              audit.ID,
		"subject_type":    audit.Subject.Type,
		"subject_id":      audit.Subject.ID,
		"action_key":      audit.ActionKey,
		"resource_type":   audit.Resource.Type,
		"resource_id":     audit.Resource.ID,
		"scope_type":      audit.Scope.Type,
		"scope_id":        audit.Scope.ID,
		"request_context": string(requestContext),
		"decision":        string(audit.Decision),
		"reason_code":     audit.ReasonCode,
		"policy_version":  audit.PolicyVersion,
		"explanation":     string(explanation),
		"created_at":      audit.CreatedAt,
	}
}

func outboxEventArgs(event entity.OutboxEvent) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":             event.ID,
		"event_type":     event.EventType,
		"schema_version": event.SchemaVersion,
		"aggregate_type": event.AggregateType,
		"aggregate_id":   event.AggregateID,
		"payload":        string(event.Payload),
		"occurred_at":    event.OccurredAt,
		"published_at":   nullableTime(event.PublishedAt),
	}
}

func nullableUUID(id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	return *id
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}

func withBaseArgs(base entity.Base, args pgx.NamedArgs) pgx.NamedArgs {
	args["id"] = base.ID
	args["version"] = base.Version
	args["created_at"] = base.CreatedAt
	args["updated_at"] = base.UpdatedAt
	return args
}

func nullableCommandID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}

func commandLookupIdempotencyKey(identity query.CommandIdentity) string {
	if identity.CommandID != uuid.Nil {
		return ""
	}
	return identity.IdempotencyKey
}
