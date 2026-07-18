package admin

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// ProvisionRuntimeDatabaseRole создаёт или ужесточает login для прикладного DML.
// Операция выполняется только отдельной migration identity и не выдаёт DDL.
func ProvisionRuntimeDatabaseRole(ctx context.Context, migrationDSN string, roleName string, password string) error {
	roleName = strings.TrimSpace(roleName)
	if roleName == "" || password == "" {
		return fmt.Errorf("runtime database role and password are required")
	}
	db, err := sql.Open("pgx", migrationDSN)
	if err != nil {
		return fmt.Errorf("open migration database for runtime role provisioning: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin runtime role provisioning: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
select
	pg_catalog.set_config('matter_codex.provision_runtime_role', $1, true),
	pg_catalog.set_config('matter_codex.provision_runtime_password', $2, true)
`, roleName, password); err != nil {
		return fmt.Errorf("stage runtime role provisioning input: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
do $provision$
declare
	runtime_role_name text := pg_catalog.current_setting('matter_codex.provision_runtime_role');
	runtime_role_password text := pg_catalog.current_setting('matter_codex.provision_runtime_password');
begin
	if runtime_role_name = current_user then
		raise exception 'runtime database role must differ from migration role' using errcode = 'invalid_authorization_specification';
	end if;
	if exists (
		select 1
		from pg_catalog.pg_roles runtime_role
		join pg_catalog.pg_auth_members membership on membership.member = runtime_role.oid
		where runtime_role.rolname = runtime_role_name
	) then
		raise exception 'runtime database role must not have role memberships' using errcode = 'invalid_authorization_specification';
	end if;
	if not exists (select 1 from pg_catalog.pg_roles where rolname = runtime_role_name) then
		execute pg_catalog.format(
			'create role %I login password %L nosuperuser nocreatedb nocreaterole noinherit noreplication nobypassrls',
			runtime_role_name,
			runtime_role_password
		);
	else
		execute pg_catalog.format(
			'alter role %I with login password %L nosuperuser nocreatedb nocreaterole noinherit noreplication nobypassrls',
			runtime_role_name,
			runtime_role_password
		);
	end if;
end
$provision$;
`); err != nil {
		return fmt.Errorf("provision least-privilege runtime database role: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `select pg_catalog.set_config('matter_codex.provision_runtime_password', '', true)`); err != nil {
		return fmt.Errorf("clear runtime role provisioning input: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit runtime role provisioning: %w", err)
	}
	return nil
}

// ValidateRuntimeDatabaseRole проверяет identity базы данных для прикладного DML.
// Владельцы схемы, привилегированные роли и роли с возможностью создать временные shadow-объекты отклоняются.
func ValidateRuntimeDatabaseRole(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("runtime database pool is required")
	}
	const statement = `
select
	current_user,
	role.rolsuper,
	role.rolbypassrls,
	role.rolcreaterole,
	role.rolcreatedb,
	role.rolreplication,
	has_database_privilege(current_user, current_database(), 'TEMP'),
	has_schema_privilege(current_user, current_schema(), 'CREATE'),
	exists (
		select 1
		from pg_namespace namespace
		where namespace.nspname = current_schema()
			and namespace.nspowner = role.oid
	),
	exists (
		select 1
		from pg_class relation
		join pg_namespace namespace on namespace.oid = relation.relnamespace
		where namespace.nspname = current_schema()
			and relation.relname like 'matter_codex_%'
			and relation.relowner = role.oid
	),
	exists (
		select 1
		from pg_catalog.pg_roles assumable
		where assumable.rolname <> current_user
			and pg_catalog.pg_has_role(current_user, assumable.oid, 'MEMBER')
	)
from pg_roles role
where role.rolname = current_user`
	var (
		roleName                                      string
		superuser, bypassRLS                          bool
		createRole, createDB, replication             bool
		temporary, createSchema                       bool
		ownsSchema, ownsRelation, assumableMembership bool
	)
	if err := pool.QueryRow(ctx, statement).Scan(
		&roleName, &superuser, &bypassRLS, &createRole, &createDB, &replication,
		&temporary, &createSchema, &ownsSchema, &ownsRelation, &assumableMembership,
	); err != nil {
		return fmt.Errorf("inspect runtime database role: %w", err)
	}
	if superuser || bypassRLS || createRole || createDB || replication || temporary || createSchema || ownsSchema || ownsRelation || assumableMembership {
		return fmt.Errorf("runtime database role %q violates the least-privilege contract", roleName)
	}
	return nil
}
