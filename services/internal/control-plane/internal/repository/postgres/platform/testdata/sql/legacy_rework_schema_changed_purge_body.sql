-- Только disposable DDL: прежние graph guards остаются на месте, но real body меняет результат.
DO $fixture$
DECLARE definition text;
BEGIN
 SELECT pg_get_functiondef('control_plane.purge_project_database(uuid,uuid,text)'::regprocedure) INTO definition;
 IF regexp_count(definition,'RETURN v_deleted_rows;')<>1 THEN RAISE EXCEPTION 'fixture purge source changed'; END IF;
 EXECUTE replace(definition,'RETURN v_deleted_rows;','RETURN 0;');
END $fixture$;
