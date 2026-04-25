package accessgraph

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/codex-k8s/kodex/libs/go/postgres"
	enumtypes "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	domainrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/accessgraph"
)

const (
	bootstrapOwnerOrganizationSlug = "platform-owner"
	bootstrapOwnerOrganizationName = "Организация платформы"
)

var (
	//go:embed sql/ensure_bootstrap_owner_organization_membership.sql
	queryEnsureBootstrapOwnerOrganizationMembership string
	//go:embed sql/list_organizations.sql
	queryListOrganizations string
	//go:embed sql/list_groups.sql
	queryListGroups string
	//go:embed sql/list_organization_memberships.sql
	queryListOrganizationMemberships string
	//go:embed sql/list_user_group_memberships.sql
	queryListUserGroupMemberships string
)

// Repository stores access graph records in PostgreSQL.
type Repository struct {
	db *pgxpool.Pool
}

type listQuerySpec[T any] struct {
	query             string
	listErrContext    string
	scanErrContext    string
	iterateErrContext string
	scan              func(rows pgx.Rows) (T, error)
}

// NewRepository constructs PostgreSQL access graph repository.
func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return 200
	}
	return limit
}

func collectRows[T any](
	rows pgx.Rows,
	scan func() (T, error),
	scanErrContext string,
	iterateErrContext string,
) ([]T, error) {
	defer rows.Close()

	var out []T
	for rows.Next() {
		item, err := scan()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", scanErrContext, err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", iterateErrContext, err)
	}
	return out, nil
}

func queryRows[T any](
	ctx context.Context,
	db *pgxpool.Pool,
	query string,
	listErrContext string,
	scanErrContext string,
	iterateErrContext string,
	scan func(rows pgx.Rows) (T, error),
	args ...any,
) ([]T, error) {
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", listErrContext, err)
	}
	return collectRows(rows, func() (T, error) {
		return scan(rows)
	}, scanErrContext, iterateErrContext)
}

func listWithLimit[T any](ctx context.Context, db *pgxpool.Pool, limit int, spec listQuerySpec[T]) ([]T, error) {
	return queryRows(
		ctx,
		db,
		spec.query,
		spec.listErrContext,
		spec.scanErrContext,
		spec.iterateErrContext,
		spec.scan,
		normalizeLimit(limit),
	)
}

// EnsureBootstrapOwnerOrganizationMembership creates the canonical owner
// organization when missing and grants owner membership to the bootstrap user.
func (r *Repository) EnsureBootstrapOwnerOrganizationMembership(ctx context.Context, userID string) error {
	return postgres.ExecOrWrap(
		ctx,
		r.db,
		queryEnsureBootstrapOwnerOrganizationMembership,
		"ensure bootstrap owner organization membership",
		bootstrapOwnerOrganizationSlug,
		bootstrapOwnerOrganizationName,
		userID,
		string(enumtypes.OrganizationMembershipRoleOwner),
	)
}

// ListOrganizations returns organizations from the access foundation.
func (r *Repository) ListOrganizations(ctx context.Context, limit int) ([]domainrepo.Organization, error) {
	return listWithLimit(ctx, r.db, limit, organizationListSpec)
}

// ListGroups returns user groups from the access foundation.
func (r *Repository) ListGroups(ctx context.Context, limit int) ([]domainrepo.UserGroup, error) {
	return listWithLimit(ctx, r.db, limit, groupListSpec)
}

// ListOrganizationMemberships returns organization memberships joined with user emails.
func (r *Repository) ListOrganizationMemberships(ctx context.Context, limit int) ([]domainrepo.OrganizationMembershipView, error) {
	return listWithLimit(ctx, r.db, limit, organizationMembershipListSpec)
}

// ListUserGroupMemberships returns group memberships joined with user emails.
func (r *Repository) ListUserGroupMemberships(ctx context.Context, limit int) ([]domainrepo.UserGroupMembershipView, error) {
	return listWithLimit(ctx, r.db, limit, userGroupMembershipListSpec)
}

var _ domainrepo.Repository = (*Repository)(nil)

var organizationListSpec = listQuerySpec[domainrepo.Organization]{
	query:             queryListOrganizations,
	listErrContext:    "list organizations",
	scanErrContext:    "scan organization",
	iterateErrContext: "iterate organizations",
	scan: func(rows pgx.Rows) (domainrepo.Organization, error) {
		var item domainrepo.Organization
		return item, rows.Scan(&item.ID, &item.Slug, &item.Name)
	},
}

var groupListSpec = listQuerySpec[domainrepo.UserGroup]{
	query:             queryListGroups,
	listErrContext:    "list groups",
	scanErrContext:    "scan group",
	iterateErrContext: "iterate groups",
	scan: func(rows pgx.Rows) (domainrepo.UserGroup, error) {
		var (
			item           domainrepo.UserGroup
			organizationID pgtype.Text
			scope          string
		)
		if err := rows.Scan(&item.ID, &organizationID, &scope, &item.Slug, &item.Name); err != nil {
			return domainrepo.UserGroup{}, err
		}
		if organizationID.Valid {
			item.OrganizationID = &organizationID.String
		}
		item.Scope = enumtypes.UserGroupScope(scope)
		return item, nil
	},
}

var organizationMembershipListSpec = listQuerySpec[domainrepo.OrganizationMembershipView]{
	query:             queryListOrganizationMemberships,
	listErrContext:    "list organization memberships",
	scanErrContext:    "scan organization membership",
	iterateErrContext: "iterate organization memberships",
	scan: func(rows pgx.Rows) (domainrepo.OrganizationMembershipView, error) {
		var item domainrepo.OrganizationMembershipView
		return item, rows.Scan(&item.OrganizationID, &item.UserID, &item.Email, &item.Role)
	},
}

var userGroupMembershipListSpec = listQuerySpec[domainrepo.UserGroupMembershipView]{
	query:             queryListUserGroupMemberships,
	listErrContext:    "list user group memberships",
	scanErrContext:    "scan user group membership",
	iterateErrContext: "iterate user group memberships",
	scan: func(rows pgx.Rows) (domainrepo.UserGroupMembershipView, error) {
		var item domainrepo.UserGroupMembershipView
		return item, rows.Scan(&item.GroupID, &item.UserID, &item.Email)
	},
}
