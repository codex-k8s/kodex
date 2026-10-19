-- +goose Up
SET ROLE control_plane_owner;

-- Предикат relation_id должен быть константой текущего ребра/таблицы, чтобы
-- индекс (transaction_id,relation_id,...) не перечитывал весь большой граф.
-- Удаление по устойчивому PK сохраняет строки после FK SET NULL, но сравнение
-- JSON от каждой victim со всем графом заменяется типизированным PK join.
-- Полномочия, exact graph fingerprint, блокировки и receipt не меняются.
-- +goose StatementBegin
DO $migration$
DECLARE definition text; marker text; replacement text;
BEGIN
 SELECT pg_get_functiondef('control_plane.purge_project_database(uuid,uuid,text)'::regprocedure) INTO definition;

 marker:='target.transaction_id = $1 AND target.relation_id = parent.tableoid::oid';
 IF (length(definition)-length(replace(definition,marker,'')))/length(marker)<>1 THEN
   RAISE EXCEPTION 'project purge parent index precondition failed'; END IF;
 definition:=replace(definition,marker,'target.transaction_id = $1 AND target.relation_id = $2 AND target.relation_id = parent.tableoid::oid');
 marker:=E'v_row_key, v_edge.child_table, v_edge.parent_table, v_join)\n            USING v_transaction_id;';
 IF strpos(definition,marker)=0 THEN RAISE EXCEPTION 'project purge edge arguments precondition failed'; END IF;
 definition:=replace(definition,marker,E'v_row_key, v_edge.child_table, v_edge.parent_table, v_join)\n            USING v_transaction_id, v_edge.confrelid;');

 marker:='target.transaction_id = $1 AND target.relation_id = victim.tableoid::oid';
 IF (length(definition)-length(replace(definition,marker,'')))/length(marker)<>3 THEN
   RAISE EXCEPTION 'project purge victim index precondition failed'; END IF;
 definition:=replace(definition,marker,'target.transaction_id = $1 AND target.relation_id = $3 AND target.relation_id = victim.tableoid::oid');
 definition:=replace(definition,'INTO v_bad USING v_transaction_id, p_organization_id;',
   'INTO v_bad USING v_transaction_id, p_organization_id, v_table.relation_id;');
 definition:=replace(definition,'INTO v_bad USING v_transaction_id, p_project_id;',
   'INTO v_bad USING v_transaction_id, p_project_id, v_table.relation_id;');

 marker:=$marker$SELECT string_agg(format('victim.%I', key_attribute.attname), ', ' ORDER BY key_column.n)$marker$;
 replacement:=$marker$SELECT string_agg(format('victim.%I = (target.row_key->>%s)::%s', key_attribute.attname,
            key_column.n-1, format_type(key_attribute.atttypid,key_attribute.atttypmod)), ' AND ' ORDER BY key_column.n)$marker$;
 IF strpos(definition,marker)=0 THEN RAISE EXCEPTION 'project purge primary key join precondition failed'; END IF;
 definition:=replace(definition,marker,replacement);
 marker:=$marker$'WHERE target.transaction_id = $1 AND target.relation_id = $3 AND target.relation_id = victim.tableoid::oid '
            'AND target.row_key = jsonb_build_array(%s)'$marker$;
 replacement:=$marker$'WHERE target.transaction_id = $1 AND target.relation_id = $2 AND target.relation_id = victim.tableoid::oid '
            'AND %s'$marker$;
 IF strpos(definition,marker)=0 THEN RAISE EXCEPTION 'project purge delete join precondition failed'; END IF;
 definition:=replace(definition,marker,replacement);
 marker:=E'v_table.table_name, v_row_key)\n        USING v_transaction_id;';
 IF strpos(definition,marker)=0 THEN RAISE EXCEPTION 'project purge delete arguments precondition failed'; END IF;
 definition:=replace(definition,marker,E'v_table.table_name, v_row_key)\n        USING v_transaction_id, v_table.relation_id;');
 EXECUTE definition;
END $migration$;
-- +goose StatementEnd

RESET ROLE;
