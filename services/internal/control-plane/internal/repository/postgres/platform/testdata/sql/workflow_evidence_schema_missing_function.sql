-- Только fault injection в отдельной disposable schema; production данные не изменяются.
DROP FUNCTION control_plane.workflow_evidence_execution_assigned(uuid,uuid,uuid);
