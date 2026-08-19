-- +goose Up
-- One-shot fence принадлежит migration job и не становится compatibility path.
-- +goose StatementBegin
DO $$
BEGIN
	IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'matter_codex_migration') THEN
		CREATE ROLE matter_codex_migration
			NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
	END IF;
END
$$;
-- +goose StatementEnd
ALTER ROLE matter_codex_migration
	NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;

CREATE TABLE matter_codex_legacy_data_cutovers (
	plan_id text PRIMARY KEY CHECK (plan_id ~ '^[a-z0-9][a-z0-9._-]{15,127}$'),
	plan_sha256 text NOT NULL CHECK (plan_sha256 ~ '^[a-f0-9]{64}$'),
	source_sha256 text NOT NULL CHECK (source_sha256 ~ '^[a-f0-9]{64}$'),
	target_sha256 text NOT NULL CHECK (target_sha256 ~ '^[a-f0-9]{64}$'),
	backup_sha256 text NOT NULL CHECK (backup_sha256 ~ '^[a-f0-9]{64}$'),
	manifest_sha256 text NOT NULL CHECK (manifest_sha256 ~ '^[a-f0-9]{64}$'),
	materialization_sha256 text NOT NULL CHECK (materialization_sha256 ~ '^[a-f0-9]{64}$'),
	materialization_count bigint NOT NULL CHECK (materialization_count BETWEEN 0 AND 100000),
	restore_verified boolean NOT NULL DEFAULT false,
	state text NOT NULL CHECK (state IN ('PREPARED', 'FROZEN', 'COMMITTED', 'ABORTED')),
	prepared_at timestamptz NOT NULL,
	frozen_at timestamptz,
	committed_at timestamptz,
	aborted_at timestamptz,
	CHECK ((state IN ('FROZEN', 'COMMITTED')) = (frozen_at IS NOT NULL)),
	CHECK (state NOT IN ('FROZEN', 'COMMITTED') OR restore_verified),
	CHECK ((state = 'COMMITTED') = (committed_at IS NOT NULL)),
	CHECK ((state = 'ABORTED') = (aborted_at IS NOT NULL))
);
CREATE UNIQUE INDEX matter_codex_legacy_data_cutovers_one_winner_uidx
	ON matter_codex_legacy_data_cutovers ((true))
	WHERE state IN ('FROZEN', 'COMMITTED');

-- Единственный closed-set inventory используется snapshot, locks, grants и
-- selective pg_dump. Prefix scan служит только fail-closed проверкой drift.
CREATE FUNCTION matter_codex_legacy_source_tables()
RETURNS TABLE(table_name text)
LANGUAGE sql
IMMUTABLE
SET search_path = pg_catalog
AS $$
	VALUES
		('matter_codex_agent_delegation_callback_deliveries'),
		('matter_codex_agent_delegation_callback_delivery_manifests'),
		('matter_codex_agent_delegations'),
		('matter_codex_agent_flows'),
		('matter_codex_agent_profiles'),
		('matter_codex_agent_prompt_templates'),
		('matter_codex_agent_role_runtime_variables'),
		('matter_codex_agent_roles'),
		('matter_codex_agent_runs'),
		('matter_codex_agent_session_turns'),
		('matter_codex_agent_sessions'),
		('matter_codex_audit_events'),
		('matter_codex_automation_audit_events'),
		('matter_codex_automation_schedules'),
		('matter_codex_chat_participants'),
		('matter_codex_chat_repositories'),
		('matter_codex_chats'),
		('matter_codex_cluster_admin_bindings'),
		('matter_codex_cluster_admin_bot_bindings'),
		('matter_codex_cluster_admin_delivery_fences'),
		('matter_codex_cluster_admin_dependencies'),
		('matter_codex_cluster_admin_prompt_templates'),
		('matter_codex_cluster_admin_revocations'),
		('matter_codex_cluster_admin_runtime_variable_bindings'),
		('matter_codex_cluster_admin_session_bindings'),
		('matter_codex_cluster_admin_subjects'),
		('matter_codex_credentials'),
		('matter_codex_github_accounts'),
		('matter_codex_interaction_capabilities'),
		('matter_codex_mattermost_bot_identities'),
		('matter_codex_memory_embeddings'),
		('matter_codex_memory_record_versions'),
		('matter_codex_memory_records'),
		('matter_codex_openai_accounts'),
		('matter_codex_owner_attention_requests'),
		('matter_codex_policy_revisions'),
		('matter_codex_process_runs'),
		('matter_codex_process_turns'),
		('matter_codex_project_repositories'),
		('matter_codex_project_runtime_variables'),
		('matter_codex_projects'),
		('matter_codex_repositories'),
		('matter_codex_role_capabilities'),
		('matter_codex_role_relationship_policies'),
		('matter_codex_runtime_agent_binding_discoveries'),
		('matter_codex_runtime_agent_binding_outbox'),
		('matter_codex_schedule_occurrences'),
		('matter_codex_scheduled_runs'),
		('matter_codex_thread_contexts'),
		('matter_codex_work_claims')
$$;

-- Возвращает канонически упорядоченный полный снимок всех legacy-таблиц.
-- Payload содержит чувствительные данные и предназначен только для потокового
-- SHA-256 внутри job; он никогда не входит в отчёт или runtime log.
-- +goose StatementBegin
CREATE FUNCTION matter_codex_legacy_snapshot_rows()
RETURNS TABLE(table_name text, row_payload text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
	source_table record;
BEGIN
	FOR source_table IN
		SELECT inventory.table_name AS name
		FROM public.matter_codex_legacy_source_tables() AS inventory
		ORDER BY inventory.table_name
	LOOP
		table_name := source_table.name;
		row_payload := NULL;
		RETURN NEXT;
		RETURN QUERY EXECUTE format(
			'SELECT %L::text, to_jsonb(source_row)::text FROM public.%I AS source_row ORDER BY to_jsonb(source_row)::text COLLATE "C"',
			source_table.name,
			source_table.name
		);
	END LOOP;
END
$$;
-- +goose StatementEnd

-- Один общий guard закрывает запись во все существующие legacy-таблицы после
-- FROZEN. ABORTED до target cutover снова разрешает legacy writer.
-- +goose StatementBegin
CREATE FUNCTION matter_codex_reject_writes_after_cutover()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
	IF EXISTS (
		SELECT 1
		FROM public.matter_codex_legacy_data_cutovers
		WHERE state IN ('FROZEN', 'COMMITTED')
	) THEN
		RAISE EXCEPTION 'legacy data cutover fence rejects business writes'
			USING ERRCODE = '55000';
	END IF;
	RETURN CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
END
$$;

CREATE FUNCTION matter_codex_lock_legacy_business_tables()
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
	source_table record;
BEGIN
	FOR source_table IN
		SELECT inventory.table_name AS name
		FROM public.matter_codex_legacy_source_tables() AS inventory
		ORDER BY inventory.table_name
	LOOP
		IF NOT EXISTS (
			SELECT 1
			FROM pg_catalog.pg_trigger AS trigger
			JOIN pg_catalog.pg_class AS guarded_table ON guarded_table.oid = trigger.tgrelid
			JOIN pg_catalog.pg_namespace AS guarded_namespace ON guarded_namespace.oid = guarded_table.relnamespace
			WHERE guarded_namespace.nspname = 'public'
				AND guarded_table.relname = source_table.name
				AND trigger.tgname = 'matter_codex_legacy_cutover_guard'
				AND trigger.tgfoid = 'public.matter_codex_reject_writes_after_cutover()'::pg_catalog.regprocedure
				AND trigger.tgenabled IN ('O', 'A')
				AND trigger.tgtype = 62
				AND NOT trigger.tgisinternal
		) THEN
			RAISE EXCEPTION 'legacy data cutover guard is missing for table %', source_table.name
				USING ERRCODE = '55000';
		END IF;
		EXECUTE format('LOCK TABLE public.%I IN SHARE MODE', source_table.name);
	END LOOP;
END
$$;

DO $$
DECLARE
	source_table record;
BEGIN
	FOR source_table IN
		SELECT inventory.table_name AS name
		FROM public.matter_codex_legacy_source_tables() AS inventory
		ORDER BY inventory.table_name
	LOOP
		EXECUTE format(
			'CREATE TRIGGER matter_codex_legacy_cutover_guard BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON public.%I FOR EACH STATEMENT EXECUTE FUNCTION public.matter_codex_reject_writes_after_cutover()',
			source_table.name
		);
	END LOOP;
END
$$;
-- +goose StatementEnd

ALTER FUNCTION matter_codex_legacy_snapshot_rows() OWNER TO CURRENT_USER;
ALTER FUNCTION matter_codex_reject_writes_after_cutover() OWNER TO CURRENT_USER;
ALTER FUNCTION matter_codex_lock_legacy_business_tables() OWNER TO CURRENT_USER;
ALTER FUNCTION matter_codex_legacy_source_tables() OWNER TO CURRENT_USER;

-- +goose StatementBegin
DO $$
DECLARE
	source_table record;
	sequence_name record;
BEGIN
	IF EXISTS (
		(SELECT class.relname
		 FROM pg_catalog.pg_class AS class
		 JOIN pg_catalog.pg_namespace AS namespace ON namespace.oid = class.relnamespace
		 WHERE namespace.nspname = 'public'
			AND class.relkind IN ('r', 'p')
			AND NOT class.relispartition
			AND class.relname LIKE 'matter_codex_%'
			-- Metadata прежнего мигратора не содержит предметных данных и не
			-- входит в snapshot/backup/cutover graph.
			AND class.relname <> 'matter_codex_schema_migrations'
			AND class.relname <> 'matter_codex_legacy_data_cutovers'
		 EXCEPT SELECT table_name FROM public.matter_codex_legacy_source_tables())
		UNION ALL
		(SELECT table_name FROM public.matter_codex_legacy_source_tables()
		 EXCEPT
		 SELECT class.relname
		 FROM pg_catalog.pg_class AS class
		 JOIN pg_catalog.pg_namespace AS namespace ON namespace.oid = class.relnamespace
		 WHERE namespace.nspname = 'public'
			AND class.relkind IN ('r', 'p')
			AND NOT class.relispartition)
	) THEN
		RAISE EXCEPTION 'legacy source inventory differs from the reviewed closed set'
			USING ERRCODE = '55000';
	END IF;
	REVOKE ALL ON matter_codex_legacy_data_cutovers FROM PUBLIC;
	REVOKE ALL ON FUNCTION matter_codex_legacy_source_tables() FROM PUBLIC;
	REVOKE ALL ON FUNCTION matter_codex_legacy_snapshot_rows() FROM PUBLIC;
	REVOKE ALL ON FUNCTION matter_codex_reject_writes_after_cutover() FROM PUBLIC;
	REVOKE ALL ON FUNCTION matter_codex_lock_legacy_business_tables() FROM PUBLIC;
	GRANT USAGE ON SCHEMA public TO matter_codex_migration;
	GRANT SELECT, INSERT, UPDATE ON matter_codex_legacy_data_cutovers TO matter_codex_migration;
	GRANT EXECUTE ON FUNCTION matter_codex_legacy_snapshot_rows() TO matter_codex_migration;
	GRANT EXECUTE ON FUNCTION matter_codex_lock_legacy_business_tables() TO matter_codex_migration;
	FOR source_table IN SELECT table_name FROM public.matter_codex_legacy_source_tables() LOOP
		EXECUTE format('REVOKE ALL ON TABLE public.%I FROM matter_codex_migration', source_table.table_name);
		EXECUTE format('GRANT SELECT ON TABLE public.%I TO matter_codex_migration', source_table.table_name);
	END LOOP;
	FOR sequence_name IN
		SELECT DISTINCT sequence.relname AS name
		FROM pg_catalog.pg_class AS sequence
		JOIN pg_catalog.pg_namespace AS namespace ON namespace.oid = sequence.relnamespace
		JOIN pg_catalog.pg_depend AS dependency ON dependency.objid = sequence.oid
		JOIN pg_catalog.pg_class AS source_table ON source_table.oid = dependency.refobjid
		JOIN public.matter_codex_legacy_source_tables() AS inventory
			ON inventory.table_name = source_table.relname
		WHERE namespace.nspname = 'public'
			AND sequence.relkind = 'S'
			AND dependency.deptype IN ('a', 'i')
	LOOP
		EXECUTE format('REVOKE ALL ON SEQUENCE public.%I FROM matter_codex_migration', sequence_name.name);
		EXECUTE format('GRANT SELECT ON SEQUENCE public.%I TO matter_codex_migration', sequence_name.name);
	END LOOP;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
	RAISE EXCEPTION 'migration 000041 is forward-only: cutover fence and receipts cannot be removed safely';
END
$$;
-- +goose StatementEnd
