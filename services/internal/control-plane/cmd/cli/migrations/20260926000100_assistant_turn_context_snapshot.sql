-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.runs
    ADD COLUMN assistant_context_route text NOT NULL DEFAULT '',
    ADD COLUMN assistant_context_entity_kind text NOT NULL DEFAULT '',
    ADD COLUMN assistant_context_entity_ref text NOT NULL DEFAULT '';

UPDATE control_plane.runs run
SET assistant_context_route = conversation.context_route,
    assistant_context_entity_kind = conversation.context_entity_kind,
    assistant_context_entity_ref = conversation.context_entity_ref
FROM control_plane.assistant_conversations conversation
WHERE run.organization_id = conversation.organization_id
  AND run.session_id = conversation.session_id
  AND run.target_type = 'SYSTEM_ASSISTANT';

ALTER TABLE control_plane.runs
    ADD CONSTRAINT runs_assistant_context_route_length
        CHECK (char_length(assistant_context_route) <= 500),
    ADD CONSTRAINT runs_assistant_context_entity_kind_length
        CHECK (char_length(assistant_context_entity_kind) <= 80),
    ADD CONSTRAINT runs_assistant_context_entity_ref_length
        CHECK (char_length(assistant_context_entity_ref) <= 96),
    ADD CONSTRAINT runs_assistant_context_entity_pair
        CHECK ((assistant_context_entity_kind = '') = (assistant_context_entity_ref = ''));

RESET ROLE;
