-- Fault injection только в отдельной disposable schema; signature/OID/ACL не меняются.
DO $fault$ DECLARE v_definition text; v_source text; v_replacement text:=E'\n SELECT true\n'; BEGIN
 SELECT pg_get_functiondef(oid),prosrc INTO STRICT v_definition,v_source FROM pg_proc
 WHERE oid='control_plane.workflow_evidence_execution_assigned(uuid,uuid,uuid)'::regprocedure;
 IF length(v_definition)-length(replace(v_definition,v_source,''))<>length(v_source) THEN RAISE EXCEPTION 'fault injection requires exact function definition'; END IF;
 EXECUTE replace(v_definition,v_source,v_replacement);
END $fault$;
