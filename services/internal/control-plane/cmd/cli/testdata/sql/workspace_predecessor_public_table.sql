-- Только отдельная disposable БД; изменение откатывается после отрицательной проверки.
GRANT SELECT ON control_plane.session_workspace_storage TO PUBLIC;
