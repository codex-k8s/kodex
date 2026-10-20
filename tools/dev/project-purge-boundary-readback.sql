-- name: project_purge_boundary_readback :many
-- Только TEMP граф в read-only snapshot; persistent rows/locks не изменяются.
CREATE TEMP TABLE purge_boundary_targets(relation_id oid NOT NULL,row_tid tid NOT NULL,PRIMARY KEY(relation_id,row_tid));
CREATE TEMP TABLE purge_boundary_root(id uuid NOT NULL,organization_id uuid NOT NULL);
BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='90s';
-- Идентификатор служит только read-only диагностике, не выдаёт authority.
INSERT INTO purge_boundary_root
 SELECT id,organization_id FROM control_plane.projects
 WHERE ref='prj_XM2a_cP83D3Fl3gM2xIcbjZh' AND lifecycle='PURGE_PENDING';
INSERT INTO purge_boundary_targets
 SELECT p.tableoid,p.ctid FROM control_plane.projects p JOIN purge_boundary_root r ON r.id=p.id;
DO $diagnostic$
DECLARE edge record; selected_table record; join_columns text; added integer; total_added integer; null_count bigint; foreign_count bigint; root_id uuid;
BEGIN
 SELECT id INTO root_id FROM purge_boundary_root;
 IF root_id IS NULL THEN RETURN; END IF;
 LOOP
   total_added:=0;
   ANALYZE purge_boundary_targets;
   FOR edge IN SELECT con.conrelid,con.confrelid,con.conkey,con.confkey,
       con.conrelid::regclass AS child_table,con.confrelid::regclass AS parent_table
     FROM pg_constraint con WHERE con.contype='f' AND con.connamespace='control_plane'::regnamespace
       AND con.confrelid IN (SELECT DISTINCT relation_id FROM purge_boundary_targets)
     ORDER BY con.conrelid,con.conname
   LOOP
     SELECT string_agg(format('child.%I=parent.%I',ca.attname,pa.attname),' AND ' ORDER BY cc.n) INTO join_columns
     FROM unnest(edge.conkey) WITH ORDINALITY cc(number,n)
     JOIN unnest(edge.confkey) WITH ORDINALITY pc(number,n) USING(n)
     JOIN pg_attribute ca ON ca.attrelid=edge.conrelid AND ca.attnum=cc.number
     JOIN pg_attribute pa ON pa.attrelid=edge.confrelid AND pa.attnum=pc.number;
     EXECUTE format('INSERT INTO purge_boundary_targets SELECT child.tableoid,child.ctid FROM %s child JOIN %s parent ON %s JOIN purge_boundary_targets target ON target.relation_id=$1 AND target.relation_id=parent.tableoid AND target.row_tid=parent.ctid ON CONFLICT DO NOTHING',edge.child_table,edge.parent_table,join_columns) USING edge.confrelid;
     GET DIAGNOSTICS added=ROW_COUNT;
     total_added:=total_added+added;
   END LOOP;
   EXIT WHEN total_added=0;
 END LOOP;
 FOR selected_table IN SELECT DISTINCT target.relation_id,target.relation_id::regclass AS table_name
   FROM purge_boundary_targets target JOIN pg_attribute a ON a.attrelid=target.relation_id AND a.attname='project_id' AND NOT a.attisdropped
 LOOP
   EXECUTE format('SELECT count(*) FILTER (WHERE row.project_id IS NULL),count(*) FILTER (WHERE row.project_id IS NOT NULL AND row.project_id<>$1) FROM %s row JOIN purge_boundary_targets target ON target.relation_id=$2 AND target.relation_id=row.tableoid AND target.row_tid=row.ctid',selected_table.table_name)
   INTO null_count,foreign_count USING root_id,selected_table.relation_id;
   IF null_count+foreign_count>0 THEN
     RAISE NOTICE '%',jsonb_build_object('table',selected_table.table_name::text,'nullProjectRows',null_count,'foreignProjectRows',foreign_count);
   END IF;
 END LOOP;
END $diagnostic$;
COMMIT;
