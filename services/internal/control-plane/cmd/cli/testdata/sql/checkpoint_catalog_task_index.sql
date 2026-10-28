-- Только откатываемая схема отдельной одноразовой базы.
CREATE INDEX checkpoint_unexpected ON control_plane.session_archive_tasks(ref);
