-- Fault injection: возвращает точное тело012 и тем самым удаляет обе ветки013.
DO $fault$ DECLARE definition text; newer text; older text; BEGIN
 SELECT pg_get_functiondef('control_plane.workflow_runtime_artifact_pins(uuid,uuid)'::regprocedure) INTO definition;
 newer:=E' UNION SELECT * FROM control_plane.workflow_exception_artifact_pins(p_tenant,p_node)\n UNION SELECT * FROM control_plane.workflow_rework_artifact_pins(p_tenant,p_node)\n UNION SELECT pin.* FROM consumer CROSS JOIN LATERAL control_plane.workflow_input_artifact_pins(p_tenant,consumer.root_run_id,consumer.workflow_inputs) pin;';
 older:=E' UNION SELECT * FROM control_plane.workflow_exception_artifact_pins(p_tenant,p_node);';
 IF length(definition)-length(replace(definition,newer,''))<>length(newer) THEN RAISE EXCEPTION 'exact013 reader required'; END IF;
 EXECUTE replace(definition,newer,older);
END $fault$;
