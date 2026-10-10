WITH RECURSIVE nodes(relation_id) AS (
 SELECT 'control_plane.projects'::regclass::oid UNION SELECT c.conrelid FROM nodes
 JOIN pg_constraint c ON c.contype='f' AND c.confrelid=nodes.relation_id
 JOIN pg_namespace n ON n.oid=c.connamespace AND n.nspname='control_plane'
), edge AS (
 SELECT count(*) total,md5(string_agg(cn.nspname||'.'||cc.relname||'|'||c.conname||'|'||pn.nspname||'.'||pc.relname||'|'||c.confdeltype::text,E'\n' ORDER BY cn.nspname,cc.relname,c.conname)) fingerprint
 FROM pg_constraint c JOIN pg_class cc ON cc.oid=c.conrelid JOIN pg_namespace cn ON cn.oid=cc.relnamespace
 JOIN pg_class pc ON pc.oid=c.confrelid JOIN pg_namespace pn ON pn.oid=pc.relnamespace
 WHERE c.contype='f' AND c.confrelid IN(SELECT relation_id FROM nodes) AND c.conrelid IN(SELECT relation_id FROM nodes)
)
SELECT count(*),md5(string_agg(n.nspname||'.'||c.relname,E'\n' ORDER BY n.nspname,c.relname)),edge.total,edge.fingerprint
FROM nodes JOIN pg_class c ON c.oid=nodes.relation_id JOIN pg_namespace n ON n.oid=c.relnamespace CROSS JOIN edge
GROUP BY edge.total,edge.fingerprint;
