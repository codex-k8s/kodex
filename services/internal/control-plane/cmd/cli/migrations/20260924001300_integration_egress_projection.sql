-- +goose Up
SET ROLE control_plane_owner;
-- Поколение проекции остаётся в owner-БД: несколько control-plane replicas
-- не могут повторно опубликовать устаревший сетевой допуск после рестарта.
CREATE TABLE control_plane.integration_egress_projection (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    generation bigint NOT NULL DEFAULT 0 CHECK (generation >= 0),
    target_digest text NOT NULL DEFAULT '' CHECK (target_digest = '' OR target_digest ~ '^[a-f0-9]{64}$'),
    document jsonb,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK ((generation = 0 AND target_digest = '' AND document IS NULL)
        OR (generation > 0 AND target_digest <> '' AND jsonb_typeof(document) = 'object'))
);
INSERT INTO control_plane.integration_egress_projection(singleton) VALUES (true);
GRANT SELECT, UPDATE ON control_plane.integration_egress_projection TO control_plane_runtime;
RESET ROLE;
