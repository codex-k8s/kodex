-- +goose Up
SET ROLE control_plane_owner;

-- Новый NO ACTION FK внутри owner-графа должен поддерживать уже существующий
-- SET CONSTRAINTS ALL DEFERRED. Обычный commit всё так же проверяет FK.
-- +goose StatementBegin
DO $migration$
DECLARE item record; missing integer; definition text; marker text; replacement text;
BEGIN
 WITH RECURSIVE nodes(relation_id) AS (
   SELECT 'control_plane.projects'::regclass::oid UNION
   SELECT c.conrelid FROM nodes n JOIN pg_constraint c ON c.confrelid=n.relation_id
     AND c.contype='f' AND c.connamespace='control_plane'::regnamespace
 ) SELECT count(*) INTO missing FROM pg_constraint
 WHERE contype='f' AND confdeltype='a' AND NOT condeferrable
   AND conrelid IN(SELECT relation_id FROM nodes) AND confrelid IN(SELECT relation_id FROM nodes);
 IF missing<>12 THEN RAISE EXCEPTION 'project purge deferred graph precondition failed'; END IF;
 FOR item IN WITH RECURSIVE nodes(relation_id) AS (
   SELECT 'control_plane.projects'::regclass::oid UNION
   SELECT c.conrelid FROM nodes n JOIN pg_constraint c ON c.confrelid=n.relation_id
     AND c.contype='f' AND c.connamespace='control_plane'::regnamespace
 ) SELECT conrelid::regclass AS relation_name,conname FROM pg_constraint
   WHERE contype='f' AND confdeltype='a' AND NOT condeferrable
     AND conrelid IN('control_plane.required_workflow_launches'::regclass,'control_plane.prepared_content_bindings'::regclass)
     AND conrelid IN(SELECT relation_id FROM nodes) AND confrelid IN(SELECT relation_id FROM nodes)
   ORDER BY conrelid,conname
 LOOP
   EXECUTE format('ALTER TABLE %s ALTER CONSTRAINT %I DEFERRABLE INITIALLY IMMEDIATE',item.relation_name,item.conname);
 END LOOP;
 WITH RECURSIVE nodes(relation_id) AS (
   SELECT 'control_plane.projects'::regclass::oid UNION
   SELECT c.conrelid FROM nodes n JOIN pg_constraint c ON c.confrelid=n.relation_id
     AND c.contype='f' AND c.connamespace='control_plane'::regnamespace
 ) SELECT count(*) INTO missing FROM pg_constraint
 WHERE contype='f' AND confdeltype='a' AND NOT condeferrable
   AND conrelid IN(SELECT relation_id FROM nodes) AND confrelid IN(SELECT relation_id FROM nodes);
 IF missing<>0 THEN RAISE EXCEPTION 'project purge deferred graph is incomplete'; END IF;

 SELECT pg_get_functiondef('control_plane.purge_project_database(uuid,uuid,text)'::regprocedure) INTO definition;
 marker:='    v_transaction_id := pg_current_xact_id();';
 IF (length(definition)-length(replace(definition,marker,'')))/length(marker)<>1 THEN
   RAISE EXCEPTION 'project purge deferred guard precondition failed'; END IF;
 replacement:=$patch$
    IF EXISTS (
      WITH RECURSIVE nodes(relation_id) AS (
        SELECT 'control_plane.projects'::regclass::oid UNION
        SELECT c.conrelid FROM nodes n JOIN pg_constraint c ON c.confrelid=n.relation_id
          AND c.contype='f' AND c.connamespace='control_plane'::regnamespace
      ) SELECT 1 FROM pg_constraint WHERE contype='f' AND confdeltype='a' AND NOT condeferrable
        AND conrelid IN(SELECT relation_id FROM nodes) AND confrelid IN(SELECT relation_id FROM nodes)
    ) THEN RAISE EXCEPTION 'project purge deferred foreign-key graph changed'; END IF;
$patch$ || marker;
 EXECUTE replace(definition,marker,replacement);
END $migration$;
-- +goose StatementEnd

RESET ROLE;
