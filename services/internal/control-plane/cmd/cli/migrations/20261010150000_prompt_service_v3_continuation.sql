-- +goose Up
SET ROLE control_plane_owner;

-- Уже сохранённые v2 notices и их immutable digests не изменяются.
-- Новый producer и consumer v3 устанавливаются совместно с этим closed CHECK.
ALTER TABLE control_plane.session_continuation_notices
    DROP CONSTRAINT session_continuation_notices_service_template_revision_check;
ALTER TABLE control_plane.session_continuation_notices
    ADD CONSTRAINT session_continuation_notices_service_template_revision_check
    CHECK (service_template_revision IN ('prompt-service-v2', 'prompt-service-v3'));

RESET ROLE;
