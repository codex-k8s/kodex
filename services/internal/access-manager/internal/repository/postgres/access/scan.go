package access

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/value"
)

func scanOrganization(row postgreslib.RowScanner) (entity.Organization, error) {
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
	organization.ParentOrganizationID = postgreslib.UUIDPtrFromPG(parentOrganizationID)
	return organization, err
}

func scanUser(row postgreslib.RowScanner) (entity.User, error) {
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

func scanCommandResult(row postgreslib.RowScanner) (entity.CommandResult, error) {
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
	if id := postgreslib.UUIDPtrFromPG(commandID); id != nil {
		result.CommandID = *id
	}
	return result, err
}

func scanAllowlistEntry(row postgreslib.RowScanner) (entity.AllowlistEntry, error) {
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
	entry.OrganizationID = postgreslib.UUIDPtrFromPG(organizationID)
	entry.DefaultStatus = enum.UserStatus(defaultStatus)
	entry.Status = enum.AllowlistStatus(status)
	return entry, err
}

func scanPendingAccessItem(row postgreslib.RowScanner) (entity.PendingAccessItem, error) {
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

func scanOutboxEvent(row postgreslib.RowScanner) (entity.OutboxEvent, error) {
	scanned, err := postgreslib.ScanOutboxEventRow(row)
	return entity.OutboxEvent{
		ID:                  scanned.Identity.RowID,
		EventType:           scanned.Identity.TypeName,
		SchemaVersion:       scanned.Identity.ContractVersion,
		AggregateType:       scanned.Identity.SubjectKind,
		AggregateID:         scanned.Identity.SubjectID,
		Payload:             scanned.Body,
		OccurredAt:          scanned.Identity.CreatedAt,
		PublishedAt:         scanned.Delivery.SentAt,
		AttemptCount:        scanned.Delivery.Attempts,
		NextAttemptAt:       scanned.Delivery.RetryAt,
		LockedUntil:         scanned.Delivery.LeaseUntil,
		FailedPermanentlyAt: scanned.Failure.DeadAt,
		FailureKind:         scanned.Failure.FailureCode,
		LastError:           scanned.Failure.ErrorText,
	}, err
}

func scanGroup(row postgreslib.RowScanner) (entity.Group, error) {
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
	group.ScopeID = postgreslib.UUIDPtrFromPG(scopeID)
	group.ParentGroupID = postgreslib.UUIDPtrFromPG(parentGroupID)
	group.Status = enum.GroupStatus(status)
	return group, err
}

func scanMembership(row postgreslib.RowScanner) (entity.Membership, error) {
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

func scanExternalProvider(row postgreslib.RowScanner) (entity.ExternalProvider, error) {
	var scanned externalProviderRow
	return scanned.scan(row)
}

func scanExternalAccount(row postgreslib.RowScanner) (entity.ExternalAccount, error) {
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
	account.SecretBindingRefID = postgreslib.UUIDPtrFromPG(secretBindingRefID)
	return account, err
}

func scanExternalAccountBinding(row postgreslib.RowScanner) (entity.ExternalAccountBinding, error) {
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

func scanSecretBindingRef(row postgreslib.RowScanner) (entity.SecretBindingRef, error) {
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
	secret.RotatedAt = postgreslib.TimePtrFromPG(rotatedAt)
	return secret, err
}

func scanPackageInstallationSecretRef(row postgreslib.RowScanner) (entity.PackageInstallationSecretRef, error) {
	var ref entity.PackageInstallationSecretRef
	var status, storeType string
	var metadata []byte
	var rotatedAt pgtype.Timestamptz
	err := row.Scan(
		&ref.ID,
		&ref.PackageInstallationID,
		&ref.InstallationScope.Type,
		&ref.InstallationScope.ID,
		&ref.LogicalKey,
		&status,
		&metadata,
		&ref.Version,
		&ref.CreatedAt,
		&ref.UpdatedAt,
		&ref.SecretRef.ID,
		&storeType,
		&ref.SecretRef.StoreRef,
		&ref.SecretRef.ValueFingerprint,
		&rotatedAt,
		&ref.SecretRef.Version,
		&ref.SecretRef.CreatedAt,
		&ref.SecretRef.UpdatedAt,
	)
	if err != nil {
		return entity.PackageInstallationSecretRef{}, err
	}
	ref.Status = enum.PackageInstallationSecretRefStatus(status)
	ref.SecretRef.StoreType = enum.SecretStoreType(storeType)
	ref.SecretRef.RotatedAt = postgreslib.TimePtrFromPG(rotatedAt)
	if err := json.Unmarshal(metadata, &ref.Metadata); err != nil {
		return entity.PackageInstallationSecretRef{}, err
	}
	if ref.Metadata == nil {
		ref.Metadata = map[string]string{}
	}
	return ref, nil
}

func scanAccessAction(row postgreslib.RowScanner) (entity.AccessAction, error) {
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

func scanAccessRule(row postgreslib.RowScanner) (entity.AccessRule, error) {
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

func scanAccessDecisionAudit(row postgreslib.RowScanner) (entity.AccessDecisionAudit, error) {
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

func (row *externalProviderRow) scan(scanner postgreslib.RowScanner) (entity.ExternalProvider, error) {
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
