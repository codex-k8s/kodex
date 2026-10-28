-- Только откатываемая схема отдельной одноразовой базы.
ALTER TABLE control_plane.session_archives DROP CONSTRAINT session_archives_publication_key;
