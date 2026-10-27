-- Только собственная disposable schema: отрицательная проверка настоящего release SQL.
GRANT EXECUTE ON FUNCTION control_plane.legacy_rework_execution_assigned(uuid,uuid) TO PUBLIC;
