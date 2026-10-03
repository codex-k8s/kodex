WITH RECURSIVE nodes(relation_id) AS (
    SELECT 'control_plane.projects'::regclass::oid
    UNION
    SELECT constraint_row.conrelid FROM nodes
    JOIN pg_constraint constraint_row ON constraint_row.contype='f' AND constraint_row.confrelid=nodes.relation_id
    JOIN pg_namespace namespace_row ON namespace_row.oid=constraint_row.connamespace AND namespace_row.nspname='control_plane'
), node_digest AS (
    SELECT count(*) AS count, md5(string_agg(namespace_row.nspname || '.' || class_row.relname,
        E'\n' ORDER BY namespace_row.nspname,class_row.relname)) AS digest
    FROM nodes JOIN pg_class class_row ON class_row.oid=nodes.relation_id
    JOIN pg_namespace namespace_row ON namespace_row.oid=class_row.relnamespace
), edge_digest AS (
    SELECT count(*) AS count, md5(string_agg(child_namespace.nspname || '.' || child_class.relname || '|' ||
        constraint_row.conname || '|' || parent_namespace.nspname || '.' || parent_class.relname || '|' || constraint_row.confdeltype::text,
        E'\n' ORDER BY child_namespace.nspname,child_class.relname,constraint_row.conname)) AS digest
    FROM pg_constraint constraint_row
    JOIN pg_class child_class ON child_class.oid=constraint_row.conrelid
    JOIN pg_namespace child_namespace ON child_namespace.oid=child_class.relnamespace
    JOIN pg_class parent_class ON parent_class.oid=constraint_row.confrelid
    JOIN pg_namespace parent_namespace ON parent_namespace.oid=parent_class.relnamespace
    WHERE constraint_row.contype='f' AND constraint_row.confrelid IN (SELECT relation_id FROM nodes)
      AND constraint_row.conrelid IN (SELECT relation_id FROM nodes)
)
SELECT node_digest.count,node_digest.digest,edge_digest.count,edge_digest.digest FROM node_digest,edge_digest;
