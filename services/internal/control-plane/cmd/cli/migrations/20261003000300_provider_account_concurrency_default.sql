-- +goose Up
SET ROLE control_plane_owner;

-- Значение относится только к новым аккаунтам; текущие меняются штатной командой.
ALTER TABLE control_plane.provider_accounts
    ALTER COLUMN max_concurrent_executions SET DEFAULT 10;

RESET ROLE;
