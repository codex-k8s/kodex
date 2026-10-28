-- Только отдельная disposable БД; изменение откатывается после отрицательной проверки.
DO $fixture$
DECLARE v_table regclass; v_trigger name;
BEGIN
 SELECT t.tgrelid::regclass,t.tgname INTO STRICT v_table,v_trigger
 FROM pg_trigger t JOIN pg_constraint c ON c.oid=t.tgconstraint
 WHERE c.conrelid='control_plane.session_workspace_storage'::regclass AND c.contype='f'
   AND t.tgrelid<>c.conrelid ORDER BY c.conname,t.tgname LIMIT 1;
 EXECUTE format('ALTER TABLE %s DISABLE TRIGGER %I',v_table,v_trigger);
END $fixture$;
