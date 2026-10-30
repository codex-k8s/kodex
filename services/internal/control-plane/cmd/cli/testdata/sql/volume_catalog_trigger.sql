-- Обратимое изменение только disposable schema; business rows не затрагиваются.
ALTER TABLE control_plane.session_workspace_volumes DISABLE TRIGGER workspace_volume_custody_guard;
