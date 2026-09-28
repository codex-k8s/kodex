-- +goose Up
SET ROLE control_plane_owner;

-- Новая таблица approval scopes и её внешние ключи входят в граф проекта.
-- Сверяем весь граф до замены отпечатка; неизвестное изменение схемы должно
-- по-прежнему закрыто остановить окончательное удаление.
-- +goose StatementBegin
DO $migration$
DECLARE
    v_nodes integer;
    v_node_fingerprint text;
    v_edges integer;
    v_edge_fingerprint text;
    v_definition text;
    v_old_nodes constant text := 'IF v_nodes <> 95 OR v_node_fingerprint <> ''3e28ab48263c1b206fbbfd7d6f61a5e8'' THEN';
    v_new_nodes constant text := 'IF v_nodes <> 96 OR v_node_fingerprint <> ''944cc88a40bfa8addb7930ac23c713ab'' THEN';
    v_old_edges constant text := 'IF v_edges <> 239 OR v_edge_fingerprint <> ''af29c190b44abeee6e3bfd0929f5e0a6'' THEN';
    v_new_edges constant text := 'IF v_edges <> 244 OR v_edge_fingerprint <> ''b487c79691a16a205f983e6d0adc5039'' THEN';
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
    SELECT count(*), md5(string_agg(namespace_row.nspname || '.' || class_row.relname,
           E'\n' ORDER BY namespace_row.nspname, class_row.relname))
    INTO v_nodes, v_node_fingerprint
    FROM nodes JOIN pg_class class_row ON class_row.oid = nodes.relation_id
    JOIN pg_namespace namespace_row ON namespace_row.oid = class_row.relnamespace;

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
        constraint_row.conname || '|' || parent_namespace.nspname || '.' ||
        parent_class.relname || '|' || constraint_row.confdeltype::text,
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

    IF v_nodes <> 96 OR v_node_fingerprint <> '944cc88a40bfa8addb7930ac23c713ab'
       OR v_edges <> 244 OR v_edge_fingerprint <> 'b487c79691a16a205f983e6d0adc5039' THEN
        RAISE EXCEPTION 'project purge graph migration precondition failed';
    END IF;

    SELECT pg_get_functiondef('control_plane.purge_project_database(uuid,uuid,text)'::regprocedure)
    INTO v_definition;
    IF v_definition IS NULL OR strpos(v_definition, v_old_nodes) = 0
       OR strpos(v_definition, v_old_edges) = 0
       OR strpos(substr(v_definition, strpos(v_definition, v_old_nodes) + length(v_old_nodes)), v_old_nodes) > 0
       OR strpos(substr(v_definition, strpos(v_definition, v_old_edges) + length(v_old_edges)), v_old_edges) > 0 THEN
        RAISE EXCEPTION 'project purge function migration precondition failed';
    END IF;
    EXECUTE replace(replace(v_definition, v_old_nodes, v_new_nodes), v_old_edges, v_new_edges);
END;
$migration$;
-- +goose StatementEnd

RESET ROLE;
