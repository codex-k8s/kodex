-- Только откатываемая схема отдельной одноразовой базы.
ALTER TABLE control_plane.session_archive_tasks ADD COLUMN unexpected boolean;
