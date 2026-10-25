-- +goose Up
SET ROLE control_plane_owner;
SET LOCAL lock_timeout = '8s';
SET LOCAL statement_timeout = '30s';

-- Опубликованная история не участвует в поиске блокирующего предшественника.
-- DEAD_LETTER остаётся в индексе: terminal predecessor нельзя обходить.
-- Данные, порядок, lease и durable broker receipt не меняются.
CREATE INDEX outbox_unpublished_predecessor
    ON control_plane.outbox_events (ordering_key, sequence)
    WHERE state <> 'PUBLISHED';

RESET ROLE;
