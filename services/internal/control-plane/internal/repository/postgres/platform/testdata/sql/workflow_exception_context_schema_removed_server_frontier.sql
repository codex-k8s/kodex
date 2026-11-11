-- Синтетически возвращаем исчезнувшую SERVER_DAG boundary, не меняя роль/ACL.
DO $fixture$
DECLARE definition text; prefix text; first_pos integer; last_pos integer;
BEGIN
 SELECT pg_get_functiondef('control_plane.runtime_file_source_visible(uuid,uuid,uuid,uuid,uuid,text,text,uuid)'::regprocedure) INTO definition;
 prefix:=E'WHEN ''RUN_RESULT'' THEN file.source IN (''AGENT_RESULT'',''INTEGRATION_RESULT'')\n              ';
 first_pos:=strpos(definition,prefix)+length(prefix);
 last_pos:=strpos(definition,') ELSE (');
 IF first_pos<=length(prefix) OR last_pos<=first_pos THEN
   RAISE EXCEPTION 'synthetic server frontier marker missing';
 END IF;
 definition:=left(definition,first_pos-1)||'AND ('||substr(definition,last_pos+length(') ELSE ('));
 definition:=replace(definition,E'              ) END\n              AND EXISTS',E'              )\n              AND EXISTS');
 EXECUTE definition;
END $fixture$;
