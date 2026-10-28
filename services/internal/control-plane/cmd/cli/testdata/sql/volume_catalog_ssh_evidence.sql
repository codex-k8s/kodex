-- Проверка сохранения полного прежнего evidence guard в disposable БД.
DO $drift$
DECLARE v_source text;
BEGIN
 SELECT prosrc INTO STRICT v_source FROM pg_proc WHERE oid='control_plane.guard_workflow_effect_records()'::regprocedure;
 EXECUTE format('CREATE OR REPLACE FUNCTION control_plane.guard_workflow_effect_records() RETURNS trigger LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,control_plane AS %L',v_source||E'\n-- disposable evidence drift\n');
END $drift$;
