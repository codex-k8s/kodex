// Package access implements the PostgreSQL repository for access-manager.
package access

import (
	"context"
	"embed"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/errs"
	accessrepo "github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/repository/access"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/value"
)

// SQLFiles contains named SQL queries for the access-manager PostgreSQL repository.
//
//go:embed sql/*.sql
var SQLFiles embed.FS

var _ accessrepo.Repository = (*Repository)(nil)

type database interface {
	execer
	queryer
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Repository persists access-manager aggregates in PostgreSQL.
type Repository struct {
	db database
}

const (
	operationGetCommandResult                     = "domain.Repository.GetCommandResult"
	operationCreateOrganization                   = "domain.Repository.CreateOrganization"
	operationGetOrganization                      = "domain.Repository.GetOrganization"
	operationCountActiveOwnerOrganizations        = "domain.Repository.CountActiveOwnerOrganizations"
	operationCreateUser                           = "domain.Repository.CreateUser"
	operationGetUser                              = "domain.Repository.GetUser"
	operationUpdateUser                           = "domain.Repository.UpdateUser"
	operationGetUserByEmail                       = "domain.Repository.GetUserByEmail"
	operationGetUserByIdentity                    = "domain.Repository.GetUserByIdentity"
	operationListUserAccessScopes                 = "domain.Repository.ListUserAccessScopes"
	operationLinkUserIdentity                     = "domain.Repository.LinkUserIdentity"
	operationPutAllowlistEntry                    = "domain.Repository.PutAllowlistEntry"
	operationUpdateAllowlistEntry                 = "domain.Repository.UpdateAllowlistEntry"
	operationFindAllowlistEntry                   = "domain.Repository.FindAllowlistEntry"
	operationGetAllowlistEntry                    = "domain.Repository.GetAllowlistEntry"
	operationCreateGroup                          = "domain.Repository.CreateGroup"
	operationGetGroup                             = "domain.Repository.GetGroup"
	operationFindMembership                       = "domain.Repository.FindMembership"
	operationSetMembership                        = "domain.Repository.SetMembership"
	operationListMemberships                      = "domain.Repository.ListMemberships"
	operationListMembershipsByTarget              = "domain.Repository.ListMembershipsByTarget"
	operationPutExternalProvider                  = "domain.Repository.PutExternalProvider"
	operationUpdateExternalProvider               = "domain.Repository.UpdateExternalProvider"
	operationGetExternalProvider                  = "domain.Repository.GetExternalProvider"
	operationGetExternalProviderBySlug            = "domain.Repository.GetExternalProviderBySlug"
	operationRegisterExternalAccount              = "domain.Repository.RegisterExternalAccount"
	operationUpdateExternalAccount                = "domain.Repository.UpdateExternalAccount"
	operationGetExternalAccount                   = "domain.Repository.GetExternalAccount"
	operationBindExternalAccount                  = "domain.Repository.BindExternalAccount"
	operationGetExternalAccountBinding            = "domain.Repository.GetExternalAccountBinding"
	operationUpdateExternalAccountBinding         = "domain.Repository.UpdateExternalAccountBinding"
	operationFindExternalAccountBinding           = "domain.Repository.FindExternalAccountBinding"
	operationFindExternalAccountBindingByIdentity = "domain.Repository.FindExternalAccountBindingByIdentity"
	operationPutSecretBindingRef                  = "domain.Repository.PutSecretBindingRef"
	operationGetSecretBindingRef                  = "domain.Repository.GetSecretBindingRef"
	operationListPackageInstallationSecretRefs    = "domain.Repository.ListPackageInstallationSecretRefs"
	operationPutAccessAction                      = "domain.Repository.PutAccessAction"
	operationGetAccessActionByKey                 = "domain.Repository.GetAccessActionByKey"
	operationPutAccessRule                        = "domain.Repository.PutAccessRule"
	operationFindAccessRule                       = "domain.Repository.FindAccessRule"
	operationListAccessRules                      = "domain.Repository.ListAccessRules"
	operationRecordAccessDecision                 = "domain.Repository.RecordAccessDecision"
	operationGetAccessDecisionAudit               = "domain.Repository.GetAccessDecisionAudit"
	operationListPendingAccess                    = "domain.Repository.ListPendingAccess"
	operationClaimOutboxEvents                    = "domain.Repository.ClaimOutboxEvents"
	operationMarkOutboxEventPublished             = "domain.Repository.MarkOutboxEventPublished"
	operationMarkOutboxEventFailed                = "domain.Repository.MarkOutboxEventFailed"
	operationMarkOutboxEventPermanentlyFailed     = "domain.Repository.MarkOutboxEventPermanentlyFailed"
)

// NewRepository creates a PostgreSQL-backed access repository.
func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) GetCommandResult(ctx context.Context, identity query.CommandIdentity) (entity.CommandResult, error) {
	return queryOne(ctx, r.db, operationGetCommandResult, queryCommandResultGet, commandIdentityArgs(identity), scanCommandResult)
}

func (r *Repository) CreateOrganization(ctx context.Context, organization entity.Organization, event entity.OutboxEvent, result entity.CommandResult) error {
	create := mutation{Query: queryOrganizationCreate}
	create.Args = organizationArgs(organization)
	return r.createWithCommandResult(ctx, operationCreateOrganization, event, create, result)
}

func (r *Repository) GetOrganization(ctx context.Context, id uuid.UUID) (entity.Organization, error) {
	return queryOne(ctx, r.db, operationGetOrganization, queryOrganizationGetByID, pgx.NamedArgs{"id": id}, scanOrganization)
}

func (r *Repository) CountActiveOwnerOrganizations(ctx context.Context) (int, error) {
	count, err := queryExactlyOneRowTo(ctx, r.db, operationCountActiveOwnerOrganizations, queryOrganizationCountActiveOwner, nil, pgx.RowTo[int64])
	return int(count), err
}

func (r *Repository) CreateUser(ctx context.Context, user entity.User, identity entity.UserIdentity, event entity.OutboxEvent) error {
	return r.mutateWithOutbox(
		ctx,
		operationCreateUser,
		event,
		mutation{Query: queryUserCreate, Args: userArgs(user)},
		mutation{Query: queryUserIdentityCreate, Args: userIdentityArgs(identity)},
	)
}

func (r *Repository) GetUser(ctx context.Context, id uuid.UUID) (entity.User, error) {
	return queryOne(ctx, r.db, operationGetUser, queryUserGetByID, pgx.NamedArgs{"id": id}, scanUser)
}

func (r *Repository) UpdateUser(ctx context.Context, user entity.User, previousVersion int64, event entity.OutboxEvent, result *entity.CommandResult) error {
	return updateAggregateWithCommandResult(ctx, r, operationUpdateUser, queryUserUpdate, user, previousVersion, userUpdateArgs, event, result)
}

func (r *Repository) GetUserByEmail(ctx context.Context, email string) (entity.User, error) {
	return queryOne(ctx, r.db, operationGetUserByEmail, queryUserGetByEmail, pgx.NamedArgs{"primary_email": email}, scanUser)
}

func (r *Repository) GetUserByIdentity(ctx context.Context, provider enum.IdentityProvider, subject string) (entity.User, error) {
	return queryOne(ctx, r.db, operationGetUserByIdentity, queryUserGetByIdentity, userIdentityLookupArgs(string(provider), subject), scanUser)
}

func (r *Repository) ListUserAccessScopes(ctx context.Context, userID uuid.UUID) ([]value.ScopeRef, error) {
	rows, err := r.db.Query(ctx, queryUserListAccessScopes, pgx.NamedArgs{"user_id": userID})
	if err != nil {
		return nil, wrapError(operationListUserAccessScopes, err)
	}
	scopes, err := pgx.CollectRows(rows, pgx.RowToStructByPos[value.ScopeRef])
	return scopes, wrapError(operationListUserAccessScopes, err)
}

func (r *Repository) LinkUserIdentity(ctx context.Context, identity entity.UserIdentity, event entity.OutboxEvent) error {
	return r.mutateWithOutbox(ctx, operationLinkUserIdentity, event, mutation{Query: queryUserIdentityCreate, Args: userIdentityArgs(identity)})
}

func (r *Repository) PutAllowlistEntry(ctx context.Context, entry entity.AllowlistEntry, event entity.OutboxEvent) error {
	return r.mutateWithOutbox(ctx, operationPutAllowlistEntry, event, mutation{Query: queryAllowlistEntryUpsert, Args: allowlistEntryArgs(entry), RequireAffected: true})
}

func (r *Repository) UpdateAllowlistEntry(ctx context.Context, entry entity.AllowlistEntry, previousVersion int64, event entity.OutboxEvent, result *entity.CommandResult) error {
	return updateAggregateWithCommandResult(ctx, r, operationUpdateAllowlistEntry, queryAllowlistEntryUpdate, entry, previousVersion, allowlistEntryUpdateArgs, event, result)
}

func (r *Repository) FindAllowlistEntry(ctx context.Context, matchType enum.AllowlistMatchType, value string) (entity.AllowlistEntry, error) {
	return queryOne(ctx, r.db, operationFindAllowlistEntry, queryAllowlistEntryFind, allowlistLookupArgs(string(matchType), value), scanAllowlistEntry)
}

func (r *Repository) GetAllowlistEntry(ctx context.Context, id uuid.UUID) (entity.AllowlistEntry, error) {
	return queryOne(ctx, r.db, operationGetAllowlistEntry, queryAllowlistEntryGetByID, pgx.NamedArgs{"id": id}, scanAllowlistEntry)
}

func (r *Repository) CreateGroup(ctx context.Context, group entity.Group, event entity.OutboxEvent, result entity.CommandResult) error {
	create := mutation{Args: groupArgs(group)}
	create.Query = queryGroupCreate
	return r.createWithCommandResult(ctx, operationCreateGroup, event, create, result)
}

func (r *Repository) GetGroup(ctx context.Context, id uuid.UUID) (entity.Group, error) {
	return queryOne(ctx, r.db, operationGetGroup, queryGroupGetByID, pgx.NamedArgs{"id": id}, scanGroup)
}

func (r *Repository) FindMembership(ctx context.Context, identity query.MembershipIdentity) (entity.Membership, error) {
	return queryOne(ctx, r.db, operationFindMembership, queryMembershipFindByIdentity, pgx.NamedArgs{
		"subject_type": string(identity.SubjectType),
		"subject_id":   identity.SubjectID,
		"target_type":  string(identity.TargetType),
		"target_id":    identity.TargetID,
	}, scanMembership)
}

func (r *Repository) SetMembership(ctx context.Context, membership entity.Membership, event entity.OutboxEvent) error {
	return r.mutateWithOutbox(ctx, operationSetMembership, event, mutation{Query: queryMembershipUpsert, Args: membershipArgs(membership), RequireAffected: true})
}

func (r *Repository) ListMemberships(ctx context.Context, filter query.MembershipGraphFilter) ([]entity.Membership, error) {
	return r.listMembershipRowsByRef(ctx, operationListMemberships, queryMembershipListBySubject, filter.Subject, filter.Statuses, membershipRefSubject)
}

func (r *Repository) ListMembershipsByTarget(ctx context.Context, filter query.MembershipTargetFilter) ([]entity.Membership, error) {
	return r.listMembershipRowsByRef(ctx, operationListMembershipsByTarget, queryMembershipListByTarget, filter.Target, filter.Statuses, membershipRefTarget)
}

func (r *Repository) listMembershipRowsByRef(
	ctx context.Context,
	operation string,
	sql string,
	ref value.SubjectRef,
	statuses []enum.MembershipStatus,
	refKind membershipRefKind,
) ([]entity.Membership, error) {
	refID, err := uuid.Parse(ref.ID)
	if err != nil {
		return nil, wrapError(operation, errs.ErrInvalidArgument)
	}
	statusValues, err := membershipStatusValues(statuses)
	if err != nil {
		return nil, wrapError(operation, err)
	}
	return r.listMembershipRows(ctx, operation, sql, membershipRefArgs(ref, refID, statusValues, refKind))
}

func (r *Repository) listMembershipRows(ctx context.Context, operation string, sql string, args pgx.NamedArgs) ([]entity.Membership, error) {
	rows, err := r.db.Query(ctx, sql, args)
	if err != nil {
		return nil, wrapError(operation, err)
	}
	memberships, err := postgreslib.ScanRows(rows, scanMembership)
	return memberships, wrapError(operation, err)
}

func membershipStatusValues(statuses []enum.MembershipStatus) ([]string, error) {
	if len(statuses) == 0 {
		return nil, errs.ErrInvalidArgument
	}
	values := make([]string, 0, len(statuses))
	for _, status := range statuses {
		if status == "" {
			return nil, errs.ErrInvalidArgument
		}
		values = append(values, string(status))
	}
	return values, nil
}

func membershipRefArgs(ref value.SubjectRef, id uuid.UUID, statuses []string, kind membershipRefKind) pgx.NamedArgs {
	switch kind {
	case membershipRefSubject:
		return pgx.NamedArgs{"subject_type": ref.Type, "subject_id": id, "statuses": statuses}
	case membershipRefTarget:
		return pgx.NamedArgs{"target_type": ref.Type, "target_id": id, "statuses": statuses}
	default:
		return pgx.NamedArgs{"statuses": statuses}
	}
}

type membershipRefKind string

const (
	membershipRefSubject membershipRefKind = "subject"
	membershipRefTarget  membershipRefKind = "target"
)

func (r *Repository) PutExternalProvider(ctx context.Context, provider entity.ExternalProvider, event entity.OutboxEvent) error {
	return r.mutateWithOutbox(ctx, operationPutExternalProvider, event, mutation{Query: queryExternalProviderUpsert, Args: externalProviderArgs(provider), RequireAffected: true})
}

func (r *Repository) UpdateExternalProvider(ctx context.Context, provider entity.ExternalProvider, previousVersion int64, event entity.OutboxEvent, result *entity.CommandResult) error {
	return updateAggregateWithCommandResult(ctx, r, operationUpdateExternalProvider, queryExternalProviderUpdate, provider, previousVersion, externalProviderUpdateArgs, event, result)
}

func (r *Repository) GetExternalProvider(ctx context.Context, id uuid.UUID) (entity.ExternalProvider, error) {
	return queryOne(ctx, r.db, operationGetExternalProvider, queryExternalProviderGetByID, pgx.NamedArgs{"id": id}, scanExternalProvider)
}

func (r *Repository) GetExternalProviderBySlug(ctx context.Context, slug string) (entity.ExternalProvider, error) {
	return queryOne(ctx, r.db, operationGetExternalProviderBySlug, queryExternalProviderGetBySlug, pgx.NamedArgs{"slug": slug}, scanExternalProvider)
}

func (r *Repository) RegisterExternalAccount(ctx context.Context, account entity.ExternalAccount, event entity.OutboxEvent, result entity.CommandResult) error {
	create := mutation{
		Query: queryExternalAccountCreate,
		Args:  externalAccountArgs(account),
	}
	return r.createWithCommandResult(ctx, operationRegisterExternalAccount, event, create, result)
}

func (r *Repository) UpdateExternalAccount(ctx context.Context, account entity.ExternalAccount, previousVersion int64, event entity.OutboxEvent, result *entity.CommandResult) error {
	return updateAggregateWithCommandResult(ctx, r, operationUpdateExternalAccount, queryExternalAccountUpdate, account, previousVersion, externalAccountUpdateArgs, event, result)
}

func (r *Repository) GetExternalAccount(ctx context.Context, id uuid.UUID) (entity.ExternalAccount, error) {
	return queryOne(ctx, r.db, operationGetExternalAccount, queryExternalAccountGetByID, pgx.NamedArgs{"id": id}, scanExternalAccount)
}

func (r *Repository) BindExternalAccount(ctx context.Context, binding entity.ExternalAccountBinding, event entity.OutboxEvent) error {
	return r.mutateWithOutbox(ctx, operationBindExternalAccount, event, mutation{Query: queryExternalAccountBindingUpsert, Args: externalAccountBindingArgs(binding), RequireAffected: true})
}

func (r *Repository) GetExternalAccountBinding(ctx context.Context, id uuid.UUID) (entity.ExternalAccountBinding, error) {
	return queryOne(ctx, r.db, operationGetExternalAccountBinding, queryExternalAccountBindingGetByID, pgx.NamedArgs{"id": id}, scanExternalAccountBinding)
}

func (r *Repository) UpdateExternalAccountBinding(ctx context.Context, binding entity.ExternalAccountBinding, previousVersion int64, event entity.OutboxEvent, result *entity.CommandResult) error {
	return updateAggregateWithCommandResult(ctx, r, operationUpdateExternalAccountBinding, queryExternalAccountBindingUpdate, binding, previousVersion, externalAccountBindingUpdateArgs, event, result)
}

func (r *Repository) FindExternalAccountBinding(ctx context.Context, filter query.ExternalAccountUsageFilter) (entity.ExternalAccountBinding, error) {
	return queryOne(ctx, r.db, operationFindExternalAccountBinding, queryExternalAccountBindingFindForUsage, pgx.NamedArgs{
		"external_account_id": filter.ExternalAccountID,
		"usage_scope_type":    filter.UsageScope.Type,
		"usage_scope_id":      filter.UsageScope.ID,
		"action_key":          filter.ActionKey,
	}, scanExternalAccountBinding)
}

func (r *Repository) FindExternalAccountBindingByIdentity(ctx context.Context, identity query.ExternalAccountBindingIdentity) (entity.ExternalAccountBinding, error) {
	return queryOne(ctx, r.db, operationFindExternalAccountBindingByIdentity, queryExternalAccountBindingFindByIdentity, pgx.NamedArgs{
		"external_account_id": identity.ExternalAccountID,
		"usage_scope_type":    identity.UsageScope.Type,
		"usage_scope_id":      identity.UsageScope.ID,
	}, scanExternalAccountBinding)
}

func (r *Repository) PutSecretBindingRef(ctx context.Context, secret entity.SecretBindingRef, event entity.OutboxEvent) error {
	return r.mutateWithOutbox(ctx, operationPutSecretBindingRef, event, mutation{Query: querySecretBindingRefUpsert, Args: secretBindingRefArgs(secret), RequireAffected: true})
}

func (r *Repository) GetSecretBindingRef(ctx context.Context, id uuid.UUID) (entity.SecretBindingRef, error) {
	return queryOne(ctx, r.db, operationGetSecretBindingRef, querySecretBindingRefGetByID, pgx.NamedArgs{"id": id}, scanSecretBindingRef)
}

func (r *Repository) ListPackageInstallationSecretRefs(ctx context.Context, filter query.PackageInstallationSecretRefsFilter) ([]entity.PackageInstallationSecretRef, error) {
	rows, err := r.db.Query(ctx, queryPackageInstallationSecretRefList, packageInstallationSecretRefsFilterArgs(filter))
	if err != nil {
		return nil, wrapError(operationListPackageInstallationSecretRefs, err)
	}
	refs, err := postgreslib.ScanRows(rows, scanPackageInstallationSecretRef)
	return refs, wrapError(operationListPackageInstallationSecretRefs, err)
}

func (r *Repository) PutAccessAction(ctx context.Context, action entity.AccessAction, event entity.OutboxEvent) error {
	return r.mutateWithOutbox(ctx, operationPutAccessAction, event, mutation{Query: queryAccessActionUpsert, Args: accessActionArgs(action), RequireAffected: true})
}

func (r *Repository) GetAccessActionByKey(ctx context.Context, key string) (entity.AccessAction, error) {
	return queryOne(ctx, r.db, operationGetAccessActionByKey, queryAccessActionGetByKey, pgx.NamedArgs{"key": key}, scanAccessAction)
}

func (r *Repository) PutAccessRule(ctx context.Context, rule entity.AccessRule, event entity.OutboxEvent) error {
	return r.mutateWithOutbox(ctx, operationPutAccessRule, event, mutation{Query: queryAccessRuleUpsert, Args: accessRuleArgs(rule), RequireAffected: true})
}

func (r *Repository) FindAccessRule(ctx context.Context, identity query.AccessRuleIdentity) (entity.AccessRule, error) {
	return queryOne(ctx, r.db, operationFindAccessRule, queryAccessRuleFindByIdentity, pgx.NamedArgs{
		"effect":        string(identity.Effect),
		"subject_type":  string(identity.SubjectType),
		"subject_id":    identity.SubjectID,
		"action_key":    identity.ActionKey,
		"resource_type": identity.ResourceType,
		"resource_id":   identity.ResourceID,
		"scope_type":    identity.ScopeType,
		"scope_id":      identity.ScopeID,
	}, scanAccessRule)
}

func (r *Repository) ListAccessRules(ctx context.Context, filter query.AccessRuleFilter) ([]entity.AccessRule, error) {
	if len(filter.Subjects) == 0 {
		return nil, nil
	}
	subjectTypes := make([]string, 0, len(filter.Subjects))
	subjectIDs := make([]string, 0, len(filter.Subjects))
	for _, subject := range filter.Subjects {
		subjectTypes = append(subjectTypes, subject.Type)
		subjectIDs = append(subjectIDs, subject.ID)
	}
	rows, err := r.db.Query(ctx, queryAccessRuleListForCheck, pgx.NamedArgs{
		"subject_types": subjectTypes,
		"subject_ids":   subjectIDs,
		"action_key":    filter.ActionKey,
		"resource_type": filter.ResourceType,
		"resource_id":   filter.ResourceID,
		"scope_type":    filter.Scope.Type,
		"scope_id":      filter.Scope.ID,
	})
	if err != nil {
		return nil, wrapError(operationListAccessRules, err)
	}
	rules, err := postgreslib.ScanRows(rows, scanAccessRule)
	return rules, wrapError(operationListAccessRules, err)
}

func (r *Repository) RecordAccessDecision(ctx context.Context, audit entity.AccessDecisionAudit, event *entity.OutboxEvent) error {
	return r.withTx(ctx, operationRecordAccessDecision, func(tx pgx.Tx) error {
		requestContext, err := json.Marshal(audit.RequestContext)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(audit.Explanation)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, queryAccessDecisionAuditCreate, accessDecisionAuditArgs(audit, requestContext, payload)); err != nil {
			return err
		}
		if event != nil {
			return insertOutboxEvent(ctx, tx, *event)
		}
		return nil
	})
}

func (r *Repository) GetAccessDecisionAudit(ctx context.Context, id uuid.UUID) (entity.AccessDecisionAudit, error) {
	return queryOne(ctx, r.db, operationGetAccessDecisionAudit, queryAccessDecisionAuditGetByID, pgx.NamedArgs{"id": id}, scanAccessDecisionAudit)
}

func (r *Repository) ListPendingAccess(ctx context.Context, filter query.PendingAccessFilter) ([]entity.PendingAccessItem, error) {
	rows, err := r.db.Query(ctx, queryPendingAccessList, pgx.NamedArgs{
		"scope_type": filter.Scope.Type,
		"scope_id":   filter.Scope.ID,
		"limit":      filter.Limit,
		"offset":     filter.Offset,
	})
	if err != nil {
		return nil, wrapError(operationListPendingAccess, err)
	}
	items, err := postgreslib.ScanRows(rows, scanPendingAccessItem)
	return items, wrapError(operationListPendingAccess, err)
}

func (r *Repository) ClaimOutboxEvents(ctx context.Context, limit int, now time.Time, lockedUntil time.Time) ([]entity.OutboxEvent, error) {
	args, ok := postgreslib.OutboxClaimArgs(limit, now, lockedUntil)
	if !ok {
		return nil, wrapError(operationClaimOutboxEvents, errs.ErrInvalidArgument)
	}
	rows, err := r.db.Query(ctx, queryOutboxEventClaim, args)
	if err != nil {
		return nil, wrapError(operationClaimOutboxEvents, err)
	}
	events, err := postgreslib.ScanRows(rows, scanOutboxEvent)
	return events, wrapError(operationClaimOutboxEvents, err)
}

func (r *Repository) MarkOutboxEventPublished(ctx context.Context, id uuid.UUID, attemptCount int, publishedAt time.Time) error {
	ok, err := postgreslib.ExecOutboxPublished(ctx, r.db, queryOutboxEventMarkPublished, id, attemptCount, publishedAt)
	if !ok {
		return wrapError(operationMarkOutboxEventPublished, errs.ErrInvalidArgument)
	}
	return wrapError(operationMarkOutboxEventPublished, err)
}

func (r *Repository) MarkOutboxEventFailed(ctx context.Context, id uuid.UUID, attemptCount int, nextAttemptAt time.Time, lastError string) error {
	return r.markOutboxEventDeliveryFailure(
		ctx,
		operationMarkOutboxEventFailed,
		queryOutboxEventMarkFailed,
		id,
		attemptCount,
		"next_attempt_at",
		nextAttemptAt,
		lastError,
	)
}

func (r *Repository) MarkOutboxEventPermanentlyFailed(ctx context.Context, id uuid.UUID, attemptCount int, failedAt time.Time, lastError string) error {
	return r.markOutboxEventDeliveryFailure(
		ctx,
		operationMarkOutboxEventPermanentlyFailed,
		queryOutboxEventMarkPermanentlyFailed,
		id,
		attemptCount,
		"failed_permanently_at",
		failedAt,
		lastError,
	)
}

func (r *Repository) markOutboxEventDeliveryFailure(
	ctx context.Context,
	operation string,
	queryText string,
	id uuid.UUID,
	attemptCount int,
	timestampName string,
	timestampValue time.Time,
	lastError string,
) error {
	ok, err := postgreslib.ExecOutboxDeliveryFailure(ctx, r.db, queryText, id, attemptCount, timestampName, timestampValue, lastError)
	if !ok {
		return wrapError(operation, errs.ErrInvalidArgument)
	}
	return wrapError(operation, err)
}

func (r *Repository) withTx(ctx context.Context, operation string, fn func(tx pgx.Tx) error) error {
	return wrapError(operation, postgreslib.WithTx(ctx, r.db, fn))
}

type mutation = postgreslib.Mutation

type updateArgsBuilder[T any] func(T, int64) pgx.NamedArgs

func (r *Repository) mutateWithOutbox(ctx context.Context, operation string, event entity.OutboxEvent, mutations ...mutation) error {
	mutations = append(mutations, mutation{Query: queryOutboxEventCreate, Args: outboxEventArgs(event), RequireAffected: true})
	return r.withTx(ctx, operation, func(tx pgx.Tx) error {
		return postgreslib.RunDistinctMutations(ctx, tx, errs.ErrConflict, mutations...)
	})
}

func (r *Repository) createWithCommandResult(ctx context.Context, operation string, event entity.OutboxEvent, create mutation, result entity.CommandResult) error {
	return r.mutateWithOutbox(ctx, operation, event, create, commandResultMutation(result))
}

func (r *Repository) updateWithCommandResult(ctx context.Context, operation string, queryText string, args pgx.NamedArgs, event entity.OutboxEvent, result *entity.CommandResult) error {
	mutations := []mutation{{Query: queryText, Args: args, RequireAffected: true}}
	mutations = appendOptionalCommandResult(mutations, result)
	return r.mutateWithOutbox(ctx, operation, event, mutations...)
}

func updateAggregateWithCommandResult[T any](
	ctx context.Context,
	repository *Repository,
	operation string,
	queryText string,
	aggregate T,
	previousVersion int64,
	args updateArgsBuilder[T],
	event entity.OutboxEvent,
	result *entity.CommandResult,
) error {
	return repository.updateWithCommandResult(ctx, operation, queryText, args(aggregate, previousVersion), event, result)
}

func commandResultMutation(result entity.CommandResult) mutation {
	return mutation{Query: queryCommandResultCreate, Args: commandResultArgs(result), RequireAffected: true}
}

func appendOptionalCommandResult(mutations []mutation, result *entity.CommandResult) []mutation {
	if result == nil {
		return mutations
	}
	return append(mutations, commandResultMutation(*result))
}

func insertOutboxEvent(ctx context.Context, db execer, event entity.OutboxEvent) error {
	return postgreslib.RunMutation(ctx, db, errs.ErrConflict, mutation{Query: queryOutboxEventCreate, Args: outboxEventArgs(event), RequireAffected: true})
}

func queryOne[T any](ctx context.Context, db queryer, operation, sql string, args pgx.NamedArgs, scan func(postgreslib.RowScanner) (T, error)) (T, error) {
	value, err := scan(db.QueryRow(ctx, sql, args))
	return value, wrapError(operation, err)
}

func queryExactlyOneRowTo[T any](ctx context.Context, db queryer, operation string, sql string, args pgx.NamedArgs, scan pgx.RowToFunc[T]) (T, error) {
	queryArgs := []any{}
	if args != nil {
		queryArgs = append(queryArgs, args)
	}
	rows, err := db.Query(ctx, sql, queryArgs...)
	if err != nil {
		var zero T
		return zero, wrapError(operation, err)
	}
	value, err := pgx.CollectExactlyOneRow(rows, scan)
	return value, wrapError(operation, err)
}
