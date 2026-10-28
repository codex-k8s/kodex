-- Только откатываемая схема отдельной одноразовой базы.
GRANT SELECT(object_key) ON control_plane.session_archives TO PUBLIC;
