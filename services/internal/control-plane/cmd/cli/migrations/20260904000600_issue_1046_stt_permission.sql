-- +goose Up
SET ROLE control_plane_owner;
INSERT INTO control_plane.permission_registry
    (permission_key, name_key, description_key, risk, allowed_scopes, resource_kinds, owner_condition_supported)
VALUES ('platform.stt.use', 'i18n:PERMISSION_PLATFORM_STT_USE_NAME',
        'i18n:PERMISSION_PLATFORM_STT_USE_DESCRIPTION', 'WRITE',
        ARRAY['ORGANIZATION'], ARRAY['ORGANIZATION'], false)
ON CONFLICT (permission_key) DO NOTHING;

RESET ROLE;
