-- +goose Up
SET ROLE control_plane_owner;

-- ctid пригоден для построения и блокировки графа в одном snapshot, но
-- ON DELETE SET NULL может создать новую версию дочерней строки до её шага
-- удаления. Поэтому дополнительно фиксируем устойчивый первичный ключ.
LOCK TABLE control_plane.project_purge_targets IN ACCESS EXCLUSIVE MODE;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM control_plane.project_purge_targets) THEN
        RAISE EXCEPTION 'project purge targets are not empty';
    END IF;
END;
$$;
-- +goose StatementEnd

ALTER TABLE control_plane.project_purge_targets
    ADD COLUMN row_key jsonb NOT NULL,
    ADD CONSTRAINT project_purge_targets_row_key_unique
        UNIQUE (transaction_id, relation_id, row_key);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.purge_project_database(
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
    v_row_key text;
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
    IF v_nodes <> 96 OR v_node_fingerprint <> '944cc88a40bfa8addb7930ac23c713ab' THEN
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
    IF v_edges <> 244 OR v_edge_fingerprint <> 'b487c79691a16a205f983e6d0adc5039' THEN
        RAISE EXCEPTION 'project purge foreign-key graph changed';
    END IF;

    v_transaction_id := pg_current_xact_id();
    INSERT INTO control_plane.project_purge_context(transaction_id, backend_pid, project_id)
    VALUES (v_transaction_id, pg_backend_pid(), p_project_id);
    INSERT INTO control_plane.project_purge_targets(
        transaction_id, relation_id, row_tid, row_key
    )
    SELECT v_transaction_id, project.tableoid::oid, project.ctid,
           jsonb_build_array(project.id)
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
            SELECT string_agg(format('child.%I', key_attribute.attname), ', ' ORDER BY key_column.n)
            INTO v_row_key
            FROM pg_constraint primary_key
            CROSS JOIN LATERAL unnest(primary_key.conkey) WITH ORDINALITY AS key_column(number, n)
            JOIN pg_attribute key_attribute ON key_attribute.attrelid = primary_key.conrelid
              AND key_attribute.attnum = key_column.number
            WHERE primary_key.contype = 'p' AND primary_key.conrelid = v_edge.conrelid;
            IF v_row_key IS NULL THEN
                RAISE EXCEPTION 'project purge primary key is missing';
            END IF;
            EXECUTE format(
                'INSERT INTO control_plane.project_purge_targets(transaction_id, relation_id, row_tid, row_key) '
                'SELECT $1, child.tableoid::oid, child.ctid, jsonb_build_array(%s) FROM %s child '
                'JOIN %s parent ON %s '
                'JOIN control_plane.project_purge_targets target ON '
                'target.transaction_id = $1 AND target.relation_id = parent.tableoid::oid '
                'AND target.row_tid = parent.ctid '
                'FOR UPDATE OF child ON CONFLICT DO NOTHING',
                v_row_key, v_edge.child_table, v_edge.parent_table, v_join)
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
        SELECT string_agg(format('victim.%I', key_attribute.attname), ', ' ORDER BY key_column.n)
        INTO v_row_key
        FROM pg_constraint primary_key
        CROSS JOIN LATERAL unnest(primary_key.conkey) WITH ORDINALITY AS key_column(number, n)
        JOIN pg_attribute key_attribute ON key_attribute.attrelid = primary_key.conrelid
          AND key_attribute.attnum = key_column.number
        WHERE primary_key.contype = 'p' AND primary_key.conrelid = v_table.relation_id;
        IF v_row_key IS NULL THEN
            RAISE EXCEPTION 'project purge primary key is missing';
        END IF;
        EXECUTE format(
            'DELETE FROM %s victim USING control_plane.project_purge_targets target '
            'WHERE target.transaction_id = $1 AND target.relation_id = victim.tableoid::oid '
            'AND target.row_key = jsonb_build_array(%s)', v_table.table_name, v_row_key)
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

RESET ROLE;
