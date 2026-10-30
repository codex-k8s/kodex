-- Обратимое изменение только disposable schema; business rows не затрагиваются.
ALTER FUNCTION control_plane.guard_workspace_volume_custody() OWNER TO postgres;
