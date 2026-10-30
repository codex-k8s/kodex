-- Обратимое изменение только disposable schema; business rows не затрагиваются.
ALTER TABLE control_plane.session_workspace_volumes DROP CONSTRAINT session_workspace_volume_ready_shape;
