-- Только отдельная disposable БД; изменение откатывается после отрицательной проверки.
REVOKE DELETE ON control_plane.session_workspace_storage FROM control_plane_runtime;
