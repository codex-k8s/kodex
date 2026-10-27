-- Только отдельная disposable БД; изменение откатывается после отрицательной проверки.
GRANT EXECUTE ON FUNCTION control_plane.guard_session_workspace_storage() TO PUBLIC;
