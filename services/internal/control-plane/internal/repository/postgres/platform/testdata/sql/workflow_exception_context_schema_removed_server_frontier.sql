-- Синтетически возвращаем исчезнувшую SERVER_DAG boundary, не меняя роль/ACL.
DO $fixture$
DECLARE definition text; target regprocedure; prefix text; first_pos integer; last_pos integer;
 closing CONSTANT text := E'              ) END\n              AND EXISTS';
BEGIN
 IF (SELECT max(version_id) FROM public.goose_db_version WHERE is_applied) = 20261121000200 THEN
   target := 'control_plane.runtime_file_source_visible_revision(uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid)'::regprocedure;
 ELSE
   target := 'control_plane.runtime_file_source_visible(uuid,uuid,uuid,uuid,uuid,text,text,uuid)'::regprocedure;
 END IF;
 SELECT pg_get_functiondef(target) INTO definition;
 prefix:=E'WHEN ''RUN_RESULT'' THEN file.source IN (''AGENT_RESULT'',''INTEGRATION_RESULT'')\n              ';
 first_pos:=strpos(definition,prefix)+length(prefix);
 last_pos:=strpos(definition,') ELSE (');
 IF first_pos<=length(prefix) OR last_pos<=first_pos OR strpos(definition,closing)<=last_pos THEN
   RAISE EXCEPTION 'synthetic server frontier marker missing';
 END IF;
 definition:=left(definition,first_pos-1)||'AND ('||substr(definition,last_pos+length(') ELSE ('));
 definition:=replace(definition,closing,E'              )\n              AND EXISTS');
 EXECUTE definition;
END $fixture$;
