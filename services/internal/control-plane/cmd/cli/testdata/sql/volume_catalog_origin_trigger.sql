-- Обратимое изменение только disposable schema; business rows не затрагиваются.
ALTER TABLE control_plane.runs DISABLE TRIGGER workspace_volume_root_closed;
