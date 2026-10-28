-- Fault injection только в disposable clone.
GRANT EXECUTE ON FUNCTION control_plane.workflow_exception_artifact_pins(uuid,uuid) TO PUBLIC;
