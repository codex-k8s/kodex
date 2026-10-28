-- Только откатываемая схема отдельной одноразовой базы.
ALTER TABLE control_plane.session_archive_tasks ENABLE ROW LEVEL SECURITY;
