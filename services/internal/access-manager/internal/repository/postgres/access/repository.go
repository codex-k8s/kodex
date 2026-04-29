// Package access implements the PostgreSQL repository for access-manager.
package access

import (
	"context"
	"embed"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/query"
)

// SQLFiles contains named SQL queries for the access-manager PostgreSQL repository.
//
//go:embed sql/*.sql
var SQLFiles embed.FS

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

// NewRepository creates a PostgreSQL-backed access repository.
func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) CreateOrganization(ctx context.Context, organization entity.Organization, event entity.OutboxEvent) error {
	return r.mutateWithOutbox(ctx, "create organization", event, mutation{query: queryOrganizationCreate, args: organizationArgs(organization)})
}

func (r *Repository) GetOrganization(ctx context.Context, id uuid.UUID) (entity.Organization, error) {
	return queryOne(ctx, r.db, "get organization", queryOrganizationGetByID, pgx.NamedArgs{"id": id}, scanOrganization)
}

func (r *Repository) CountActiveOwnerOrganizations(ctx context.Context) (int, error) {
	var count int64
	err := r.db.QueryRow(ctx, queryOrganizationCountActiveOwner).Scan(&count)
	return int(count), wrapError("count active owner organizations", err)
}

func (r *Repository) CreateUser(ctx context.Context, user entity.User, identity entity.UserIdentity, event entity.OutboxEvent) error {
	return r.mutateWithOutbox(
		ctx,
		"create user",
		event,
		mutation{query: queryUserCreate, args: userArgs(user)},
		mutation{query: queryUserIdentityCreate, args: userIdentityArgs(identity)},
	)
}

func (r *Repository) GetUser(ctx context.Context, id uuid.UUID) (entity.User, error) {
	return queryOne(ctx, r.db, "get user", queryUserGetByID, pgx.NamedArgs{"id": id}, scanUser)
}

func (r *Repository) GetUserByEmail(ctx context.Context, email string) (entity.User, error) {
	return queryOne(ctx, r.db, "get user by email", queryUserGetByEmail, pgx.NamedArgs{"primary_email": email}, scanUser)
}

func (r *Repository) GetUserByIdentity(ctx context.Context, provider enum.IdentityProvider, subject string) (entity.User, error) {
	return queryOne(ctx, r.db, "get user by identity", queryUserGetByIdentity, userIdentityLookupArgs(string(provider), subject), scanUser)
}

func (r *Repository) LinkUserIdentity(ctx context.Context, identity entity.UserIdentity, event entity.OutboxEvent) error {
	return r.mutateWithOutbox(ctx, "link user identity", event, mutation{query: queryUserIdentityCreate, args: userIdentityArgs(identity)})
}

func (r *Repository) PutAllowlistEntry(ctx context.Context, entry entity.AllowlistEntry, event entity.OutboxEvent) error {
	return r.mutateWithOutbox(ctx, "put allowlist entry", event, mutation{query: queryAllowlistEntryUpsert, args: allowlistEntryArgs(entry)})
}

func (r *Repository) FindAllowlistEntry(ctx context.Context, matchType enum.AllowlistMatchType, value string) (entity.AllowlistEntry, error) {
	return queryOne(ctx, r.db, "find allowlist entry", queryAllowlistEntryFind, allowlistLookupArgs(string(matchType), value), scanAllowlistEntry)
}

func (r *Repository) CreateGroup(ctx context.Context, group entity.Group, event entity.OutboxEvent) error {
	return r.mutateWithOutbox(ctx, "create group", event, mutation{query: queryGroupCreate, args: groupArgs(group)})
}

func (r *Repository) GetGroup(ctx context.Context, id uuid.UUID) (entity.Group, error) {
	return queryOne(ctx, r.db, "get group", queryGroupGetByID, pgx.NamedArgs{"id": id}, scanGroup)
}

func (r *Repository) FindMembership(ctx context.Context, identity query.MembershipIdentity) (entity.Membership, error) {
	return queryOne(ctx, r.db, "find membership", queryMembershipFindByIdentity, pgx.NamedArgs{
		"subject_type": string(identity.SubjectType),
		"subject_id":   identity.SubjectID,
		"target_type":  string(identity.TargetType),
		"target_id":    identity.TargetID,
	}, scanMembership)
}

func (r *Repository) SetMembership(ctx context.Context, membership entity.Membership, event entity.OutboxEvent) error {
	return r.mutateWithOutbox(ctx, "set membership", event, mutation{query: queryMembershipUpsert, args: membershipArgs(membership)})
}

func (r *Repository) ListMemberships(ctx context.Context, filter query.MembershipGraphFilter) ([]entity.Membership, error) {
	subjectID, err := uuid.Parse(filter.Subject.ID)
	if err != nil {
		return nil, wrapError("list memberships", errs.ErrInvalidArgument)
	}
	rows, err := r.db.Query(ctx, queryMembershipListBySubject, pgx.NamedArgs{
		"subject_type": filter.Subject.Type,
		"subject_id":   subjectID,
		"status":       string(filter.Status),
	})
	if err != nil {
		return nil, wrapError("list memberships", err)
	}
	memberships, err := scanRows(rows, scanMembership)
	return memberships, wrapError("list memberships", err)
}

func (r *Repository) PutExternalProvider(ctx context.Context, provider entity.ExternalProvider, event entity.OutboxEvent) error {
	return r.mutateWithOutbox(ctx, "put external provider", event, mutation{query: queryExternalProviderCreate, args: externalProviderArgs(provider)})
}

func (r *Repository) GetExternalProvider(ctx context.Context, id uuid.UUID) (entity.ExternalProvider, error) {
	return queryOne(ctx, r.db, "get external provider", queryExternalProviderGetByID, pgx.NamedArgs{"id": id}, scanExternalProvider)
}

func (r *Repository) RegisterExternalAccount(ctx context.Context, account entity.ExternalAccount, event entity.OutboxEvent) error {
	return r.mutateWithOutbox(ctx, "register external account", event, mutation{query: queryExternalAccountCreate, args: externalAccountArgs(account)})
}

func (r *Repository) GetExternalAccount(ctx context.Context, id uuid.UUID) (entity.ExternalAccount, error) {
	return queryOne(ctx, r.db, "get external account", queryExternalAccountGetByID, pgx.NamedArgs{"id": id}, scanExternalAccount)
}

func (r *Repository) BindExternalAccount(ctx context.Context, binding entity.ExternalAccountBinding, event entity.OutboxEvent) error {
	return r.mutateWithOutbox(ctx, "bind external account", event, mutation{query: queryExternalAccountBindingUpsert, args: externalAccountBindingArgs(binding)})
}

func (r *Repository) FindExternalAccountBinding(ctx context.Context, filter query.ExternalAccountUsageFilter) (entity.ExternalAccountBinding, error) {
	return queryOne(ctx, r.db, "find external account binding", queryExternalAccountBindingFindForUsage, pgx.NamedArgs{
		"external_account_id": filter.ExternalAccountID,
		"usage_scope_type":    filter.UsageScope.Type,
		"usage_scope_id":      filter.UsageScope.ID,
		"action_key":          filter.ActionKey,
	}, scanExternalAccountBinding)
}

func (r *Repository) PutSecretBindingRef(ctx context.Context, secret entity.SecretBindingRef, event entity.OutboxEvent) error {
	return r.mutateWithOutbox(ctx, "put secret binding ref", event, mutation{query: querySecretBindingRefInsert, args: secretBindingRefArgs(secret)})
}

func (r *Repository) GetSecretBindingRef(ctx context.Context, id uuid.UUID) (entity.SecretBindingRef, error) {
	return queryOne(ctx, r.db, "get secret binding ref", querySecretBindingRefGetByID, pgx.NamedArgs{"id": id}, scanSecretBindingRef)
}

func (r *Repository) PutAccessAction(ctx context.Context, action entity.AccessAction, event entity.OutboxEvent) error {
	return r.mutateWithOutbox(ctx, "put access action", event, mutation{query: queryAccessActionUpsert, args: accessActionArgs(action)})
}

func (r *Repository) GetAccessActionByKey(ctx context.Context, key string) (entity.AccessAction, error) {
	return queryOne(ctx, r.db, "get access action by key", queryAccessActionGetByKey, pgx.NamedArgs{"key": key}, scanAccessAction)
}

func (r *Repository) PutAccessRule(ctx context.Context, rule entity.AccessRule, event entity.OutboxEvent) error {
	return r.mutateWithOutbox(ctx, "put access rule", event, mutation{query: queryAccessRuleInsert, args: accessRuleArgs(rule)})
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
		return nil, wrapError("list access rules", err)
	}
	rules, err := scanRows(rows, scanAccessRule)
	return rules, wrapError("list access rules", err)
}

func (r *Repository) RecordAccessDecision(ctx context.Context, audit entity.AccessDecisionAudit, event *entity.OutboxEvent) error {
	return r.withTx(ctx, "record access decision", func(tx pgx.Tx) error {
		payload, err := json.Marshal(audit.Explanation)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, queryAccessDecisionAuditCreate, accessDecisionAuditArgs(audit, payload)); err != nil {
			return err
		}
		if event != nil {
			return insertOutboxEvent(ctx, tx, *event)
		}
		return nil
	})
}

func (r *Repository) withTx(ctx context.Context, operation string, fn func(tx pgx.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return wrapError(operation, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	if err := fn(tx); err != nil {
		return wrapError(operation, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return wrapError(operation, err)
	}
	committed = true
	return nil
}

type mutation struct {
	query string
	args  pgx.NamedArgs
}

func (r *Repository) mutateWithOutbox(ctx context.Context, operation string, event entity.OutboxEvent, mutations ...mutation) error {
	return r.withTx(ctx, operation, func(tx pgx.Tx) error {
		for _, item := range mutations {
			if _, err := tx.Exec(ctx, item.query, item.args); err != nil {
				return err
			}
		}
		return insertOutboxEvent(ctx, tx, event)
	})
}

func insertOutboxEvent(ctx context.Context, db execer, event entity.OutboxEvent) error {
	_, err := db.Exec(ctx, queryOutboxEventCreate, outboxEventArgs(event))
	return err
}

func queryOne[T any](ctx context.Context, db queryer, operation, sql string, args pgx.NamedArgs, scan func(rowScanner) (T, error)) (T, error) {
	value, err := scan(db.QueryRow(ctx, sql, args))
	return value, wrapError(operation, err)
}
