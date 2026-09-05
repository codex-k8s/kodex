-- +goose Up
SET ROLE control_plane_owner;
ALTER TABLE control_plane.assistant_conversations DROP CONSTRAINT assistant_conversations_state_check,
    ADD CONSTRAINT assistant_conversations_state_check CHECK(state IN ('ACTIVE','CLOSED','ARCHIVED'));
CREATE INDEX assistant_conversation_owner_history
    ON control_plane.assistant_conversations(organization_id,created_by,state,created_at DESC,ref DESC);
RESET ROLE;
