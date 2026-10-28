-- Только откатываемая схема отдельной одноразовой базы.
ALTER TABLE control_plane.session_archive_tasks DROP CONSTRAINT session_archive_tasks_session_id_fkey;
