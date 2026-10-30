-- Обратимое изменение только disposable schema; business rows не затрагиваются.
GRANT SELECT (ref) ON control_plane.session_workspace_volumes TO PUBLIC;
