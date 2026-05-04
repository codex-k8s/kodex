package access

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/value"
)

type rowScanner interface {
	Scan(dest ...any) error
}

func scanOrganization(row rowScanner) (entity.Organization, error) {
	var organization entity.Organization
	var kind, status string
	var parentOrganizationID pgtype.UUID
	err := row.Scan(
		&organization.ID,
		&kind,
		&organization.Slug,
		&organization.DisplayName,
		&organization.ImageAssetRef,
		&status,
		&parentOrganizationID,
		&organization.Version,
		&organization.CreatedAt,
		&organization.UpdatedAt,
	)
	organization.Kind = enum.OrganizationKind(kind)
	organization.Status = enum.OrganizationStatus(status)
	organization.ParentOrganizationID = uuidPtrFromPG(parentOrganizationID)
	return organization, err
}

func scanUser(row rowScanner) (entity.User, error) {
	var user entity.User
	var status string
	err := row.Scan(
		&user.ID,
		&user.PrimaryEmail,
		&user.DisplayName,
		&user.AvatarAssetRef,
		&status,
		&user.Locale,
		&user.Version,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	user.Status = enum.UserStatus(status)
	return user, err
}

func scanCommandResult(row rowScanner) (entity.CommandResult, error) {
	var result entity.CommandResult
	var commandID pgtype.UUID
	err := row.Scan(
		&result.Key,
		&commandID,
		&result.IdempotencyKey,
		&result.Operation,
		&result.AggregateType,
		&result.AggregateID,
		&result.CreatedAt,
	)
	if id := uuidPtrFromPG(commandID); id != nil {
		result.CommandID = *id
	}
	return result, err
}

func scanAllowlistEntry(row rowScanner) (entity.AllowlistEntry, error) {
	var entry entity.AllowlistEntry
	var matchType, defaultStatus, status string
	var organizationID pgtype.UUID
	err := row.Scan(
		&entry.ID,
		&matchType,
		&entry.Value,
		&organizationID,
		&defaultStatus,
		&status,
		&entry.Version,
		&entry.CreatedAt,
		&entry.UpdatedAt,
	)
	entry.MatchType = enum.AllowlistMatchType(matchType)
	entry.OrganizationID = uuidPtrFromPG(organizationID)
	entry.DefaultStatus = enum.UserStatus(defaultStatus)
	entry.Status = enum.AllowlistStatus(status)
	return entry, err
}

func scanPendingAccessItem(row rowScanner) (entity.PendingAccessItem, error) {
	var item entity.PendingAccessItem
	var subjectType, subjectID string
	err := row.Scan(
		&item.ItemID,
		&item.ItemType,
		&subjectType,
		&subjectID,
		&item.Status,
		&item.ReasonCode,
		&item.CreatedAt,
	)
	item.Subject = value.SubjectRef{Type: subjectType, ID: subjectID}
	return item, err
}

func scanGroup(row rowScanner) (entity.Group, error) {
	var group entity.Group
	var scopeType, status string
	var scopeID, parentGroupID pgtype.UUID
	err := row.Scan(
		&group.ID,
		&scopeType,
		&scopeID,
		&group.Slug,
		&group.DisplayName,
		&parentGroupID,
		&group.ImageAssetRef,
		&status,
		&group.Version,
		&group.CreatedAt,
		&group.UpdatedAt,
	)
	group.ScopeType = enum.GroupScopeType(scopeType)
	group.ScopeID = uuidPtrFromPG(scopeID)
	group.ParentGroupID = uuidPtrFromPG(parentGroupID)
	group.Status = enum.GroupStatus(status)
	return group, err
}

func scanMembership(row rowScanner) (entity.Membership, error) {
	var membership entity.Membership
	var subjectType, targetType, status, source string
	err := row.Scan(
		&membership.ID,
		&subjectType,
		&membership.SubjectID,
		&targetType,
		&membership.TargetID,
		&membership.RoleHint,
		&status,
		&source,
		&membership.Version,
		&membership.CreatedAt,
		&membership.UpdatedAt,
	)
	membership.SubjectType = enum.MembershipSubjectType(subjectType)
	membership.TargetType = enum.MembershipTargetType(targetType)
	membership.Status = enum.MembershipStatus(status)
	membership.Source = enum.MembershipSource(source)
	return membership, err
}

func scanRows[T any](rows pgx.Rows, scan func(rowScanner) (T, error)) ([]T, error) {
	defer rows.Close()
	var values []T
	for rows.Next() {
		value, err := scan(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func scanExternalProvider(row rowScanner) (entity.ExternalProvider, error) {
	var scanned externalProviderRow
	return scanned.scan(row)
}

func scanExternalAccount(row rowScanner) (entity.ExternalAccount, error) {
	var account entity.ExternalAccount
	var accountType, ownerScopeType, status string
	var secretBindingRefID pgtype.UUID
	err := row.Scan(
		&account.ID,
		&account.ExternalProviderID,
		&accountType,
		&account.DisplayName,
		&account.ImageAssetRef,
		&ownerScopeType,
		&account.OwnerScopeID,
		&status,
		&secretBindingRefID,
		&account.Version,
		&account.CreatedAt,
		&account.UpdatedAt,
	)
	account.AccountType = enum.ExternalAccountType(accountType)
	account.OwnerScopeType = enum.ExternalAccountScopeType(ownerScopeType)
	account.Status = enum.ExternalAccountStatus(status)
	account.SecretBindingRefID = uuidPtrFromPG(secretBindingRefID)
	return account, err
}

func scanExternalAccountBinding(row rowScanner) (entity.ExternalAccountBinding, error) {
	var scanned externalAccountBindingRow
	err := row.Scan(
		&scanned.id,
		&scanned.externalAccountID,
		&scanned.usageScopeType,
		&scanned.usageScopeID,
		&scanned.allowedActionKeys,
		&scanned.status,
		&scanned.version,
		&scanned.createdAt,
		&scanned.updatedAt,
	)
	return scanned.toEntity(), err
}

func scanSecretBindingRef(row rowScanner) (entity.SecretBindingRef, error) {
	var secret entity.SecretBindingRef
	var storeType string
	var rotatedAt pgtype.Timestamptz
	err := row.Scan(
		&secret.ID,
		&storeType,
		&secret.StoreRef,
		&secret.ValueFingerprint,
		&rotatedAt,
		&secret.Version,
		&secret.CreatedAt,
		&secret.UpdatedAt,
	)
	secret.StoreType = enum.SecretStoreType(storeType)
	secret.RotatedAt = timePtrFromPG(rotatedAt)
	return secret, err
}

func scanAccessAction(row rowScanner) (entity.AccessAction, error) {
	var action entity.AccessAction
	var status string
	err := row.Scan(
		&action.ID,
		&action.Key,
		&action.DisplayName,
		&action.Description,
		&action.ResourceType,
		&status,
		&action.Version,
		&action.CreatedAt,
		&action.UpdatedAt,
	)
	action.Status = enum.AccessActionStatus(status)
	return action, err
}

func scanAccessRule(row rowScanner) (entity.AccessRule, error) {
	var rule entity.AccessRule
	var effect, subjectType, status string
	err := row.Scan(
		&rule.ID,
		&effect,
		&subjectType,
		&rule.SubjectID,
		&rule.ActionKey,
		&rule.ResourceType,
		&rule.ResourceID,
		&rule.ScopeType,
		&rule.ScopeID,
		&rule.Priority,
		&status,
		&rule.Version,
		&rule.CreatedAt,
		&rule.UpdatedAt,
	)
	rule.Effect = enum.AccessEffect(effect)
	rule.SubjectType = enum.AccessSubjectType(subjectType)
	rule.Status = enum.AccessRuleStatus(status)
	return rule, err
}

func scanAccessDecisionAudit(row rowScanner) (entity.AccessDecisionAudit, error) {
	var audit entity.AccessDecisionAudit
	var subjectType, subjectID, resourceType, resourceID, scopeType, scopeID, decision string
	var requestContext, explanation []byte
	err := row.Scan(
		&audit.ID,
		&subjectType,
		&subjectID,
		&audit.ActionKey,
		&resourceType,
		&resourceID,
		&scopeType,
		&scopeID,
		&requestContext,
		&decision,
		&audit.ReasonCode,
		&audit.PolicyVersion,
		&explanation,
		&audit.CreatedAt,
	)
	if err != nil {
		return entity.AccessDecisionAudit{}, err
	}
	audit.Subject = value.SubjectRef{Type: subjectType, ID: subjectID}
	audit.Resource = value.ResourceRef{Type: resourceType, ID: resourceID}
	audit.Scope = value.ScopeRef{Type: scopeType, ID: scopeID}
	audit.Decision = enum.AccessDecision(decision)
	if err := json.Unmarshal(requestContext, &audit.RequestContext); err != nil {
		return entity.AccessDecisionAudit{}, err
	}
	if err := json.Unmarshal(explanation, &audit.Explanation); err != nil {
		return entity.AccessDecisionAudit{}, err
	}
	return audit, nil
}

func uuidPtrFromPG(value pgtype.UUID) *uuid.UUID {
	if !value.Valid {
		return nil
	}
	id := uuid.UUID(value.Bytes)
	return &id
}

func timePtrFromPG(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

type externalProviderRow struct {
	id           uuid.UUID
	slug         string
	kind         string
	displayName  string
	iconAssetRef string
	status       string
	version      int64
	createdAt    time.Time
	updatedAt    time.Time
}

func (row externalProviderRow) toEntity() entity.ExternalProvider {
	return entity.ExternalProvider{
		Base:         baseEntity(row.id, row.version, row.createdAt, row.updatedAt),
		Slug:         row.slug,
		ProviderKind: enum.ExternalProviderKind(row.kind),
		DisplayName:  row.displayName,
		IconAssetRef: row.iconAssetRef,
		Status:       enum.ExternalProviderStatus(row.status),
	}
}

func (row *externalProviderRow) scan(scanner rowScanner) (entity.ExternalProvider, error) {
	err := scanner.Scan(&row.id, &row.slug, &row.kind, &row.displayName, &row.iconAssetRef, &row.status, &row.version, &row.createdAt, &row.updatedAt)
	return row.toEntity(), err
}

type externalAccountBindingRow struct {
	id                uuid.UUID
	externalAccountID uuid.UUID
	usageScopeType    string
	usageScopeID      string
	allowedActionKeys []string
	status            string
	version           int64
	createdAt         time.Time
	updatedAt         time.Time
}

func (row externalAccountBindingRow) toEntity() entity.ExternalAccountBinding {
	binding := entity.ExternalAccountBinding{Base: baseEntity(row.id, row.version, row.createdAt, row.updatedAt)}
	binding.ExternalAccountID = row.externalAccountID
	binding.UsageScopeType = enum.ExternalAccountScopeType(row.usageScopeType)
	binding.UsageScopeID = row.usageScopeID
	binding.AllowedActionKeys = row.allowedActionKeys
	binding.Status = enum.ExternalAccountBindingStatus(row.status)
	return binding
}

func baseEntity(id uuid.UUID, version int64, createdAt time.Time, updatedAt time.Time) entity.Base {
	return entity.Base{ID: id, Version: version, CreatedAt: createdAt, UpdatedAt: updatedAt}
}
