-- Только fault injection в отдельной disposable schema; production данные не изменяются.
ALTER FUNCTION control_plane.workflow_evidence_execution_assigned(uuid,uuid,uuid) SECURITY DEFINER;
