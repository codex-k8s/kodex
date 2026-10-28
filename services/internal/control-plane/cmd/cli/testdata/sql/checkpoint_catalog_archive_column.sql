-- Только откатываемая схема отдельной одноразовой базы.
ALTER TABLE control_plane.session_archives ADD COLUMN unexpected boolean;
