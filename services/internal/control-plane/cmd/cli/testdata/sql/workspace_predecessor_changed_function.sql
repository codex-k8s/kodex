-- Только отдельная disposable БД; изменение откатывается после отрицательной проверки.
ALTER FUNCTION control_plane.guard_session_workspace_storage() SECURITY DEFINER;
