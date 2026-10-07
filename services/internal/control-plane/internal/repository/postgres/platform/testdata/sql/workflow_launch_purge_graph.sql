-- name: workflow_launch_purge_graph :one
WITH RECURSIVE nodes(relation_id) AS (SELECT 'control_plane.projects'::regclass::oid UNION
 SELECT c.conrelid FROM nodes JOIN pg_constraint c ON c.contype='f' AND c.confrelid=nodes.relation_id
 JOIN pg_namespace n ON n.oid=c.connamespace AND n.nspname='control_plane'),
 edges AS (
 SELECT cn.nspname||'.'||cc.relname||'|'||c.conname||'|'||pn.nspname||'.'||pc.relname||'|'||c.confdeltype::text AS item,cn.nspname,cc.relname,c.conname
 FROM pg_constraint c JOIN pg_class cc ON cc.oid=c.conrelid JOIN pg_namespace cn ON cn.oid=cc.relnamespace
 JOIN pg_class pc ON pc.oid=c.confrelid JOIN pg_namespace pn ON pn.oid=pc.relnamespace
 WHERE c.contype='f' AND c.confrelid IN (SELECT relation_id FROM nodes) AND c.conrelid IN (SELECT relation_id FROM nodes))
SELECT (SELECT count(*) FROM nodes),
 (SELECT md5(string_agg(n.nspname||'.'||c.relname,E'\n' ORDER BY n.nspname,c.relname)) FROM nodes JOIN pg_class c ON c.oid=nodes.relation_id JOIN pg_namespace n ON n.oid=c.relnamespace),
 (SELECT count(*) FROM edges),(SELECT md5(string_agg(item,E'\n' ORDER BY nspname,relname,conname)) FROM edges);
