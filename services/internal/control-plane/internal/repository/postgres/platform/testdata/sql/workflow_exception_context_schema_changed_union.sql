-- Только DDL fault injection отдельной disposable clone, не бизнес-состояние.
DO $$ DECLARE definition text; BEGIN
 SELECT pg_get_functiondef('control_plane.workflow_runtime_artifact_pins(uuid,uuid)'::regprocedure) INTO definition;
 IF position('UNION SELECT * FROM control_plane.workflow_exception_artifact_pins(p_tenant,p_node);' IN definition)=0 THEN
  RAISE EXCEPTION 'context union fixture source changed'; END IF;
 EXECUTE replace(definition,'UNION SELECT * FROM control_plane.workflow_exception_artifact_pins(p_tenant,p_node);','UNION SELECT * FROM dependency;');
END $$;
