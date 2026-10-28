-- Обратимое изменение только disposable schema; business rows не затрагиваются.
CREATE POLICY disposable_volume_policy ON control_plane.session_workspace_volumes USING (true);
