-- Обратимое изменение только disposable schema; business rows не затрагиваются.
GRANT EXECUTE ON FUNCTION control_plane.guard_workspace_volume_custody() TO PUBLIC;
