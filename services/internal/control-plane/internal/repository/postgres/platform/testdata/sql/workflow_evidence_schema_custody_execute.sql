-- Только fault injection отдельной disposable schema.
GRANT EXECUTE ON FUNCTION control_plane.workflow_evidence_retains_revision(uuid,uuid,uuid) TO control_plane_runtime;
