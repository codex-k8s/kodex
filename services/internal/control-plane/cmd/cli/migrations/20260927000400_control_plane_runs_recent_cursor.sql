-- +goose Up
SET ROLE control_plane_owner;

CREATE INDEX IF NOT EXISTS runs_org_recent_cursor
    ON control_plane.runs (organization_id, created_at DESC, ref DESC);

RESET ROLE;
