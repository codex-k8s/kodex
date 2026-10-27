-- Только fault injection в отдельной disposable schema; production данные не изменяются.
DROP TRIGGER workflow_attempt_journal_guard ON control_plane.run_nodes; CREATE TRIGGER workflow_attempt_journal_guard BEFORE UPDATE OF workflow_attempts ON control_plane.run_nodes FOR EACH ROW EXECUTE FUNCTION control_plane.guard_workflow_attempt_journal();
