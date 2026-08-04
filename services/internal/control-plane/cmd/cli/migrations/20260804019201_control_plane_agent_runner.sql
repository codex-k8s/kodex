-- +goose Up
RESET ROLE;
SET ROLE control_plane_owner;
SET search_path = pg_catalog, control_plane;

ALTER TABLE control_plane.runtime_executions
    ADD COLUMN provider_binding_id uuid,
    ADD COLUMN provider_binding_version bigint,
    ADD COLUMN provider_binding_sha256 text,
    ADD COLUMN codex_session_id text,
    ADD COLUMN codex_archive_relative_path text,
    ADD COLUMN codex_archive_sha256 text,
    ADD COLUMN codex_archive_provenance text,
    ADD COLUMN materializations jsonb NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE control_plane.runtime_executions
    ADD CONSTRAINT runtime_executions_provider_binding_check CHECK (
		(provider_binding_id IS NULL AND provider_binding_version IS NULL AND provider_binding_sha256 IS NULL)
		OR (provider_binding_id IS NOT NULL AND provider_binding_version > 0
			AND provider_binding_sha256 ~ '^[a-f0-9]{64}$')
    ),
    ADD CONSTRAINT runtime_executions_codex_session_check CHECK (
        codex_session_id IS NULL OR codex_session_id ~ '^[a-f0-9]{8}-[a-f0-9]{4}-[1-8][a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$'
    ),
    ADD CONSTRAINT runtime_executions_codex_archive_check CHECK (
        (codex_archive_relative_path IS NULL AND codex_archive_sha256 IS NULL AND codex_archive_provenance IS NULL)
        OR (codex_session_id IS NOT NULL
            AND codex_archive_relative_path ~ '^\\.matter-codex/state/codex-home/sessions/[0-9]{4}/[0-9]{2}/[0-9]{2}/rollout-[A-Za-z0-9._-]+\\.jsonl$'
            AND codex_archive_sha256 ~ '^[a-f0-9]{64}$'
            AND length(codex_archive_provenance) BETWEEN 1 AND 1024)
    ),
    ADD CONSTRAINT runtime_executions_materializations_check CHECK (
        jsonb_typeof(materializations) = 'array'
		AND jsonb_array_length(materializations) BETWEEN 0 AND 4096
    );

ALTER TABLE control_plane.interaction_delivery_work
	DROP CONSTRAINT interaction_delivery_work_inline_payload_check,
	ADD CONSTRAINT interaction_delivery_work_inline_payload_check
		CHECK (octet_length(inline_payload) <= 524288);

RESET ROLE;

-- +goose Down
RESET ROLE;
SET ROLE control_plane_owner;
SET search_path = pg_catalog, control_plane;

ALTER TABLE control_plane.runtime_executions
    DROP CONSTRAINT runtime_executions_materializations_check,
    DROP CONSTRAINT runtime_executions_codex_archive_check,
    DROP CONSTRAINT runtime_executions_codex_session_check,
    DROP CONSTRAINT runtime_executions_provider_binding_check,
    DROP COLUMN materializations,
    DROP COLUMN codex_archive_provenance,
    DROP COLUMN codex_archive_sha256,
    DROP COLUMN codex_archive_relative_path,
    DROP COLUMN codex_session_id,
    DROP COLUMN provider_binding_sha256,
    DROP COLUMN provider_binding_version,
    DROP COLUMN provider_binding_id;

ALTER TABLE control_plane.interaction_delivery_work
	DROP CONSTRAINT interaction_delivery_work_inline_payload_check,
	ADD CONSTRAINT interaction_delivery_work_inline_payload_check
		CHECK (octet_length(inline_payload) <= 163840);

RESET ROLE;
