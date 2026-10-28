-- Только отдельная disposable БД; изменение откатывается после отрицательной проверки.
ALTER TABLE control_plane.session_workspace_storage ADD COLUMN unexpected text;
