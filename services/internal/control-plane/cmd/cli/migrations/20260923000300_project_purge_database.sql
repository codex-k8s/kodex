-- +goose Up
SET ROLE control_plane_owner;

-- Квитанция переживает физическое удаление проекта, не сохраняя его имя,
-- описание, содержимое файлов и внешние адреса объектов.
CREATE TABLE control_plane.project_purge_receipts (
    project_id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    project_ref text NOT NULL,
    deleted_by uuid NOT NULL,
    state text NOT NULL CHECK (state IN ('PENDING', 'OBJECTS_CLEARED', 'DONE', 'FAILED')),
    objects_digest text CHECK (objects_digest ~ '^[a-f0-9]{64}$'),
    requested_at timestamptz NOT NULL DEFAULT statement_timestamp(),
    objects_cleared_at timestamptz,
    purged_at timestamptz,
    deleted_rows integer NOT NULL DEFAULT 0 CHECK (deleted_rows >= 0),
    CHECK ((state = 'DONE') = (purged_at IS NOT NULL)),
    CHECK (state NOT IN ('OBJECTS_CLEARED', 'DONE') OR
           (objects_digest IS NOT NULL AND objects_cleared_at IS NOT NULL))
);

CREATE INDEX project_purge_receipts_pending
    ON control_plane.project_purge_receipts (requested_at, project_id)
    WHERE state IN ('PENDING', 'FAILED', 'OBJECTS_CLEARED');

GRANT SELECT, INSERT, UPDATE ON control_plane.project_purge_receipts TO control_plane_runtime;

-- Контекст существует только внутри одной транзакции функции purge. Обычный
-- runtime не может записать его и тем самым обойти immutable DELETE triggers.
CREATE TABLE control_plane.project_purge_context (
    transaction_id xid8 PRIMARY KEY,
    backend_pid integer NOT NULL,
    project_id uuid NOT NULL
);
CREATE TABLE control_plane.project_purge_targets (
    transaction_id xid8 NOT NULL,
    relation_id oid NOT NULL,
    row_tid tid NOT NULL,
    PRIMARY KEY (transaction_id, relation_id, row_tid)
);
REVOKE ALL ON control_plane.project_purge_context FROM PUBLIC;
REVOKE ALL ON control_plane.project_purge_targets FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION control_plane.project_purge_authorized() RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog
AS $$
    SELECT EXISTS (
        SELECT 1 FROM control_plane.project_purge_context context
        WHERE context.transaction_id = pg_current_xact_id_if_assigned()
          AND context.backend_pid = pg_backend_pid()
    );
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.project_purge_authorized() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.project_purge_authorized()
    TO control_plane_runtime, control_plane_migrator;

-- Единый авторитетный список внешних следов проекта. Незавершённый snapshot
-- включён тоже: его возможный объект должен быть проверен как отсутствующий.
-- +goose StatementBegin
CREATE FUNCTION control_plane.project_purge_external_inventory(
    p_organization_id uuid, p_project_id uuid
) RETURNS TABLE(kind text, target text, version text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog
AS $$
    SELECT 'OBJECT'::text, content.object_key, content.object_version
    FROM control_plane.artifact_content content
    JOIN control_plane.artifacts artifact ON artifact.id = content.artifact_id
    WHERE artifact.organization_id = p_organization_id AND artifact.project_id = p_project_id
    UNION
    SELECT 'OBJECT', reservation.object_key, reservation.object_version
    FROM control_plane.agent_avatar_upload_reservations reservation
    WHERE reservation.organization_id = p_organization_id AND reservation.project_id = p_project_id
    UNION
    SELECT 'OBJECT', archive.object_key, archive.object_version
    FROM control_plane.session_archives archive
    WHERE archive.organization_id = p_organization_id AND archive.project_id = p_project_id
    UNION
    SELECT 'OBJECT', task.object_key, coalesce(task.object_version, '')
    FROM control_plane.session_archive_tasks task
    WHERE task.organization_id = p_organization_id AND task.project_id = p_project_id
      AND task.object_key IS NOT NULL
    UNION
    SELECT 'PVC', 'runtime-session-' || encode(substring(public.digest(convert_to(session.ref, 'UTF8'), 'sha256') from 1 for 8), 'hex'), session.ref
    FROM control_plane.sessions session
    WHERE session.organization_id = p_organization_id AND session.project_id = p_project_id;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.project_purge_external_inventory(uuid, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.project_purge_external_inventory(uuid, uuid) TO control_plane_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.project_purge_inventory_digest(
    p_organization_id uuid, p_project_id uuid
) RETURNS text LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog
AS $$
    SELECT encode(public.digest(convert_to(coalesce(jsonb_agg(jsonb_build_array(item.kind, item.target, item.version)
        ORDER BY item.kind, item.target, item.version)::text, '[]'), 'UTF8'), 'sha256'), 'hex')
    FROM control_plane.project_purge_external_inventory(p_organization_id, p_project_id) item;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.project_purge_inventory_digest(uuid, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.project_purge_inventory_digest(uuid, uuid) TO control_plane_runtime;

-- NO ACTION сохраняется: обычное удаление родителя по-прежнему отклоняется.
-- Только защищённая purge-транзакция откладывает проверку до конца удаления
-- всего точного графа. Новые неизвестные FK закрыто остановят purge.
-- +goose StatementBegin
DO $$
DECLARE
    item record;
    total integer;
BEGIN
    WITH RECURSIVE nodes(relation_id) AS (
        SELECT 'control_plane.projects'::regclass::oid
        UNION
        SELECT constraint_row.conrelid FROM nodes
        JOIN pg_constraint constraint_row ON constraint_row.contype = 'f'
          AND constraint_row.confrelid = nodes.relation_id
        JOIN pg_namespace namespace_row ON namespace_row.oid = constraint_row.connamespace
          AND namespace_row.nspname = 'control_plane'
    )
    SELECT count(*) INTO total FROM pg_constraint constraint_row
    WHERE constraint_row.contype = 'f' AND constraint_row.confdeltype = 'a'
      AND NOT constraint_row.condeferrable
      AND constraint_row.confrelid IN (SELECT relation_id FROM nodes)
      AND constraint_row.conrelid IN (SELECT relation_id FROM nodes);
    IF total <> 226 THEN
        RAISE EXCEPTION 'project purge foreign-key graph changed';
    END IF;

    FOR item IN
        WITH RECURSIVE nodes(relation_id) AS (
            SELECT 'control_plane.projects'::regclass::oid
            UNION
            SELECT constraint_row.conrelid FROM nodes
            JOIN pg_constraint constraint_row ON constraint_row.contype = 'f'
              AND constraint_row.confrelid = nodes.relation_id
            JOIN pg_namespace namespace_row ON namespace_row.oid = constraint_row.connamespace
              AND namespace_row.nspname = 'control_plane'
        )
        SELECT constraint_row.conrelid, constraint_row.conname
        FROM pg_constraint constraint_row
        WHERE constraint_row.contype = 'f' AND constraint_row.confdeltype = 'a'
          AND NOT constraint_row.condeferrable
          AND constraint_row.confrelid IN (SELECT relation_id FROM nodes)
          AND constraint_row.conrelid IN (SELECT relation_id FROM nodes)
        ORDER BY constraint_row.conrelid, constraint_row.conname
    LOOP
        EXECUTE format('ALTER TABLE %s ALTER CONSTRAINT %I DEFERRABLE INITIALLY IMMEDIATE',
                       item.conrelid::regclass, item.conname);
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- Существующие guard-триггеры продолжают действовать для всех обычных
-- команд. Исключение проверяет не caller-set GUC, а закрытую owner-таблицу
-- текущей транзакции. Список и digest закреплены для текущей схемы.
-- +goose StatementBegin
DO $$
DECLARE
    item record;
    total integer;
    fingerprint text;
    replacement text;
BEGIN
    WITH RECURSIVE nodes(relation_id) AS (
        SELECT 'control_plane.projects'::regclass::oid
        UNION
        SELECT constraint_row.conrelid FROM nodes
        JOIN pg_constraint constraint_row ON constraint_row.contype = 'f'
          AND constraint_row.confrelid = nodes.relation_id
        JOIN pg_namespace namespace_row ON namespace_row.oid = constraint_row.connamespace
          AND namespace_row.nspname = 'control_plane'
    )
    SELECT count(*), md5(string_agg(class_row.relname || '.' || trigger_row.tgname,
                                    E'\n' ORDER BY class_row.relname, trigger_row.tgname))
    INTO total, fingerprint
    FROM nodes JOIN pg_trigger trigger_row ON trigger_row.tgrelid = nodes.relation_id
    JOIN pg_class class_row ON class_row.oid = trigger_row.tgrelid
    WHERE NOT trigger_row.tgisinternal AND (trigger_row.tgtype & 8) <> 0;
    IF total <> 47 OR fingerprint <> '8f4057b16a479bb9fe6114d75ce3aea8' THEN
        RAISE EXCEPTION 'project purge trigger graph changed';
    END IF;

    FOR item IN
        WITH RECURSIVE nodes(relation_id) AS (
            SELECT 'control_plane.projects'::regclass::oid
            UNION
            SELECT constraint_row.conrelid FROM nodes
            JOIN pg_constraint constraint_row ON constraint_row.contype = 'f'
              AND constraint_row.confrelid = nodes.relation_id
            JOIN pg_namespace namespace_row ON namespace_row.oid = constraint_row.connamespace
              AND namespace_row.nspname = 'control_plane'
        )
        SELECT trigger_row.tgrelid, trigger_row.tgname,
               pg_get_triggerdef(trigger_row.oid) AS definition
        FROM nodes JOIN pg_trigger trigger_row ON trigger_row.tgrelid = nodes.relation_id
        WHERE NOT trigger_row.tgisinternal AND (trigger_row.tgtype & 8) <> 0
        ORDER BY trigger_row.tgrelid, trigger_row.tgname
    LOOP
        replacement := replace(item.definition,
            ' FOR EACH ROW EXECUTE FUNCTION ',
            ' FOR EACH ROW WHEN (NOT control_plane.project_purge_authorized()) EXECUTE FUNCTION ');
        IF replacement = item.definition OR position(' WHEN ' IN item.definition) > 0 THEN
            RAISE EXCEPTION 'project purge trigger format changed';
        END IF;
        EXECUTE format('DROP TRIGGER %I ON %s', item.tgname, item.tgrelid::regclass);
        EXECUTE replacement;
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- Все строки берутся только по фактическим FK-рёбрам от заблокированного
-- проекта. tableoid+ctid допустимы здесь лишь внутри одной транзакции:
-- найденные строки блокируются до удаления. Неизвестная схема или переход
-- на общую/чужую project/organization boundary закрыто останавливают purge.
-- Внешний S3/PVC cleanup должен уже иметь отдельную точную квитанцию.
-- +goose StatementBegin
CREATE FUNCTION control_plane.purge_project_database(
    p_organization_id uuid, p_project_id uuid, p_objects_digest text
) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog
AS $$
DECLARE
    v_transaction_id xid8;
    v_receipt control_plane.project_purge_receipts%ROWTYPE;
    v_project_ref text;
    v_nodes integer;
    v_node_fingerprint text;
    v_edges integer;
    v_edge_fingerprint text;
    v_edge record;
    v_table record;
    v_join text;
    v_added integer;
    v_cycle_added integer;
    v_deleted_rows integer;
    v_bad boolean;
BEGIN
    SELECT * INTO v_receipt
    FROM control_plane.project_purge_receipts
    WHERE organization_id = p_organization_id AND project_id = p_project_id
    FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'project purge receipt is missing';
    END IF;
    IF v_receipt.state = 'DONE' THEN
        IF v_receipt.objects_digest IS DISTINCT FROM p_objects_digest THEN
            RAISE EXCEPTION 'project purge receipt digest changed';
        END IF;
        RETURN v_receipt.deleted_rows;
    END IF;
    IF v_receipt.state <> 'OBJECTS_CLEARED' OR
       v_receipt.objects_digest IS DISTINCT FROM p_objects_digest OR
       v_receipt.objects_cleared_at IS NULL THEN
        RAISE EXCEPTION 'project purge external cleanup is unverified';
    END IF;
    SELECT ref INTO v_project_ref FROM control_plane.projects
    WHERE id = p_project_id AND organization_id = p_organization_id
      AND lifecycle = 'PURGE_PENDING'
    FOR UPDATE;
    IF NOT FOUND OR v_project_ref IS DISTINCT FROM v_receipt.project_ref THEN
        RAISE EXCEPTION 'project purge target changed';
    END IF;
    IF control_plane.project_purge_inventory_digest(p_organization_id, p_project_id)
       IS DISTINCT FROM p_objects_digest THEN
        RAISE EXCEPTION 'project purge external inventory changed';
    END IF;

    WITH RECURSIVE nodes(relation_id) AS (
        SELECT 'control_plane.projects'::regclass::oid
        UNION
        SELECT constraint_row.conrelid FROM nodes
        JOIN pg_constraint constraint_row ON constraint_row.contype = 'f'
          AND constraint_row.confrelid = nodes.relation_id
        JOIN pg_namespace namespace_row ON namespace_row.oid = constraint_row.connamespace
          AND namespace_row.nspname = 'control_plane'
    )
    SELECT count(*),
           md5(string_agg(namespace_row.nspname || '.' || class_row.relname,
                          E'\n' ORDER BY namespace_row.nspname, class_row.relname))
    INTO v_nodes, v_node_fingerprint
    FROM nodes JOIN pg_class class_row ON class_row.oid = nodes.relation_id
    JOIN pg_namespace namespace_row ON namespace_row.oid = class_row.relnamespace;
    IF v_nodes <> 95 OR v_node_fingerprint <> '3e28ab48263c1b206fbbfd7d6f61a5e8' THEN
        RAISE EXCEPTION 'project purge table graph changed';
    END IF;
    WITH RECURSIVE nodes(relation_id) AS (
        SELECT 'control_plane.projects'::regclass::oid
        UNION
        SELECT constraint_row.conrelid FROM nodes
        JOIN pg_constraint constraint_row ON constraint_row.contype = 'f'
          AND constraint_row.confrelid = nodes.relation_id
        JOIN pg_namespace namespace_row ON namespace_row.oid = constraint_row.connamespace
          AND namespace_row.nspname = 'control_plane'
    )
    SELECT count(*), md5(string_agg(
        child_namespace.nspname || '.' || child_class.relname || '|' ||
        constraint_row.conname || '|' ||
        parent_namespace.nspname || '.' || parent_class.relname || '|' ||
        constraint_row.confdeltype::text,
        E'\n' ORDER BY child_namespace.nspname, child_class.relname, constraint_row.conname))
    INTO v_edges, v_edge_fingerprint
    FROM pg_constraint constraint_row
    JOIN pg_class child_class ON child_class.oid = constraint_row.conrelid
    JOIN pg_namespace child_namespace ON child_namespace.oid = child_class.relnamespace
    JOIN pg_class parent_class ON parent_class.oid = constraint_row.confrelid
    JOIN pg_namespace parent_namespace ON parent_namespace.oid = parent_class.relnamespace
    WHERE constraint_row.contype = 'f'
      AND constraint_row.confrelid IN (SELECT relation_id FROM nodes)
      AND constraint_row.conrelid IN (SELECT relation_id FROM nodes);
    IF v_edges <> 239 OR v_edge_fingerprint <> 'af29c190b44abeee6e3bfd0929f5e0a6' THEN
        RAISE EXCEPTION 'project purge foreign-key graph changed';
    END IF;

    v_transaction_id := pg_current_xact_id();
    INSERT INTO control_plane.project_purge_context(transaction_id, backend_pid, project_id)
    VALUES (v_transaction_id, pg_backend_pid(), p_project_id);
    INSERT INTO control_plane.project_purge_targets(transaction_id, relation_id, row_tid)
    SELECT v_transaction_id, project.tableoid::oid, project.ctid
    FROM control_plane.projects project
    WHERE project.id = p_project_id AND project.organization_id = p_organization_id;
    SET CONSTRAINTS ALL DEFERRED;

    LOOP
        v_cycle_added := 0;
        FOR v_edge IN
            SELECT constraint_row.conrelid, constraint_row.confrelid,
                   constraint_row.conkey, constraint_row.confkey,
                   format('%I.%I', child_namespace.nspname, child_class.relname) AS child_table,
                   format('%I.%I', parent_namespace.nspname, parent_class.relname) AS parent_table
            FROM pg_constraint constraint_row
            JOIN pg_class child_class ON child_class.oid = constraint_row.conrelid
            JOIN pg_namespace child_namespace ON child_namespace.oid = child_class.relnamespace
            JOIN pg_class parent_class ON parent_class.oid = constraint_row.confrelid
            JOIN pg_namespace parent_namespace ON parent_namespace.oid = parent_class.relnamespace
            WHERE constraint_row.contype = 'f'
              AND child_namespace.nspname = 'control_plane'
              AND parent_namespace.nspname = 'control_plane'
              AND constraint_row.confrelid IN (
                  SELECT DISTINCT relation_id FROM control_plane.project_purge_targets
                  WHERE transaction_id = v_transaction_id)
            ORDER BY constraint_row.conrelid, constraint_row.conname
        LOOP
            SELECT string_agg(format('child.%I = parent.%I', child_attribute.attname,
                                     parent_attribute.attname), ' AND ' ORDER BY child_column.n)
            INTO v_join
            FROM unnest(v_edge.conkey) WITH ORDINALITY AS child_column(number, n)
            JOIN unnest(v_edge.confkey) WITH ORDINALITY AS parent_column(number, n)
              ON parent_column.n = child_column.n
            JOIN pg_attribute child_attribute ON child_attribute.attrelid = v_edge.conrelid
              AND child_attribute.attnum = child_column.number
            JOIN pg_attribute parent_attribute ON parent_attribute.attrelid = v_edge.confrelid
              AND parent_attribute.attnum = parent_column.number;
            IF v_join IS NULL THEN
                RAISE EXCEPTION 'project purge foreign-key columns changed';
            END IF;
            EXECUTE format(
                'INSERT INTO control_plane.project_purge_targets(transaction_id, relation_id, row_tid) '
                'SELECT $1, child.tableoid::oid, child.ctid FROM %s child '
                'JOIN %s parent ON %s '
                'JOIN control_plane.project_purge_targets target ON '
                'target.transaction_id = $1 AND target.relation_id = parent.tableoid::oid '
                'AND target.row_tid = parent.ctid '
                'FOR UPDATE OF child ON CONFLICT DO NOTHING',
                v_edge.child_table, v_edge.parent_table, v_join)
            USING v_transaction_id;
            GET DIAGNOSTICS v_added = ROW_COUNT;
            v_cycle_added := v_cycle_added + v_added;
        END LOOP;
        EXIT WHEN v_cycle_added = 0;
    END LOOP;

    FOR v_table IN
        SELECT target.relation_id,
               format('%I.%I', namespace_row.nspname, class_row.relname) AS table_name
        FROM control_plane.project_purge_targets target
        JOIN pg_class class_row ON class_row.oid = target.relation_id
        JOIN pg_namespace namespace_row ON namespace_row.oid = class_row.relnamespace
        WHERE target.transaction_id = v_transaction_id
        GROUP BY target.relation_id, namespace_row.nspname, class_row.relname
        ORDER BY target.relation_id
    LOOP
        IF EXISTS (SELECT 1 FROM pg_attribute attribute_row
                   WHERE attribute_row.attrelid = v_table.relation_id
                     AND attribute_row.attname = 'organization_id' AND NOT attribute_row.attisdropped) THEN
            EXECUTE format(
                'SELECT EXISTS (SELECT 1 FROM %s victim '
                'JOIN control_plane.project_purge_targets target ON '
                'target.transaction_id = $1 AND target.relation_id = victim.tableoid::oid '
                'AND target.row_tid = victim.ctid '
                'WHERE victim.organization_id IS DISTINCT FROM $2)', v_table.table_name)
            INTO v_bad USING v_transaction_id, p_organization_id;
            IF v_bad THEN
                RAISE EXCEPTION 'project purge crossed organization boundary';
            END IF;
        END IF;
        IF EXISTS (SELECT 1 FROM pg_attribute attribute_row
                   WHERE attribute_row.attrelid = v_table.relation_id
                     AND attribute_row.attname = 'project_id' AND NOT attribute_row.attisdropped) THEN
            EXECUTE format(
                'SELECT EXISTS (SELECT 1 FROM %s victim '
                'JOIN control_plane.project_purge_targets target ON '
                'target.transaction_id = $1 AND target.relation_id = victim.tableoid::oid '
                'AND target.row_tid = victim.ctid '
                'WHERE victim.project_id IS DISTINCT FROM $2)', v_table.table_name)
            INTO v_bad USING v_transaction_id, p_project_id;
            IF v_bad THEN
                RAISE EXCEPTION 'project purge crossed project boundary';
            END IF;
        END IF;
    END LOOP;

    SELECT count(*) - 1 INTO v_deleted_rows
    FROM control_plane.project_purge_targets WHERE transaction_id = v_transaction_id;
    FOR v_table IN
        SELECT target.relation_id,
               format('%I.%I', namespace_row.nspname, class_row.relname) AS table_name
        FROM control_plane.project_purge_targets target
        JOIN pg_class class_row ON class_row.oid = target.relation_id
        JOIN pg_namespace namespace_row ON namespace_row.oid = class_row.relnamespace
        WHERE target.transaction_id = v_transaction_id
          AND target.relation_id <> 'control_plane.projects'::regclass::oid
        GROUP BY target.relation_id, namespace_row.nspname, class_row.relname
        ORDER BY target.relation_id
    LOOP
        EXECUTE format(
            'DELETE FROM %s victim USING control_plane.project_purge_targets target '
            'WHERE target.transaction_id = $1 AND target.relation_id = victim.tableoid::oid '
            'AND target.row_tid = victim.ctid', v_table.table_name)
        USING v_transaction_id;
    END LOOP;
    DELETE FROM control_plane.projects
    WHERE id = p_project_id AND organization_id = p_organization_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'project purge target disappeared';
    END IF;
    SET CONSTRAINTS ALL IMMEDIATE;
    UPDATE control_plane.project_purge_receipts
    SET state = 'DONE', purged_at = statement_timestamp(), deleted_rows = v_deleted_rows
    WHERE project_id = p_project_id AND organization_id = p_organization_id;
    DELETE FROM control_plane.project_purge_targets WHERE transaction_id = v_transaction_id;
    DELETE FROM control_plane.project_purge_context WHERE transaction_id = v_transaction_id;
    RETURN v_deleted_rows;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.purge_project_database(uuid, uuid, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.purge_project_database(uuid, uuid, text)
    TO control_plane_runtime;

RESET ROLE;
