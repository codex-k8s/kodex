-- Fault injection: нетронутый014 helper должен оставаться частью полного predecessor proof.
DO $fault$ DECLARE definition text; BEGIN
 SELECT pg_get_functiondef('control_plane.workflow_exception_artifact_pins(uuid,uuid)'::regprocedure) INTO definition;
 EXECUTE replace(definition,'AS $function$', E'AS $function$\n/* predecessor drift */');
END $fault$;
