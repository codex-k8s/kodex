-- Проверка exact DEPLOYMENT-ветки без удаления прежней evidence-логики.
DO $drift$
DECLARE v_source text;
BEGIN
 SELECT prosrc INTO STRICT v_source FROM pg_proc WHERE oid='control_plane.guard_workflow_effect_records()'::regprocedure;
 v_source:=replace(v_source,$old$requirement.value->>'Kind'='DEPLOYMENT'$old$,$new$requirement.value->>'Kind'='DISABLED_DEPLOYMENT'$new$);
 EXECUTE format('CREATE OR REPLACE FUNCTION control_plane.guard_workflow_effect_records() RETURNS trigger LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,control_plane AS %L',v_source);
END $drift$;
