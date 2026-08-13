-- +goose Up
RESET ROLE;
SET ROLE control_plane_owner;

-- Ранние direct-production startup receipts включали одноразовую ревизию
-- WORKLOAD_READINESS_GRANT в request hash. Авторитетное TLS-состояние остается
-- в gateway_public_tls_state; удаляется только дефектный replay cache.
DELETE FROM control_plane.command_receipts
 WHERE scope IN ('prepare_gateway_public_tls', 'confirm_gateway_public_tls');

-- Один interaction-gateway обслуживает все проекты организации. Независимый
-- cursor выбирает partition на owner-side; bearer не закрепляется за первым
-- проектом и не влияет на fairness automation-scheduler.
CREATE TABLE control_plane.interaction_gateway_cursors (
    organization_id uuid NOT NULL,
    operation text NOT NULL CHECK (operation IN (
        'OWNER_GATE_CLAIM',
        'OWNER_GATE_EXPIRE',
        'DELIVERY_CLAIM'
    )),
    last_project_id uuid,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id, operation)
);
ALTER TABLE control_plane.interaction_gateway_cursors OWNER TO control_plane_owner;
ALTER TABLE control_plane.interaction_gateway_cursors ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.interaction_gateway_cursors FORCE ROW LEVEL SECURITY;
CREATE POLICY interaction_gateway_cursors_scope
    ON control_plane.interaction_gateway_cursors
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND (control_plane.runtime_scope()).project_id IS NULL
    )
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND (control_plane.runtime_scope()).project_id IS NULL
    );
GRANT SELECT, INSERT, UPDATE ON control_plane.interaction_gateway_cursors TO control_plane_runtime;

-- +goose StatementBegin
DO $$
DECLARE
    stored_version bigint;
BEGIN
    SELECT version
      INTO stored_version
      FROM control_plane.schema_state
     WHERE singleton = true
     FOR UPDATE;

    IF stored_version <> 20260813000200 THEN
        RAISE EXCEPTION 'control-plane readiness receipt identity source version is invalid'
            USING ERRCODE = '55000';
    END IF;

    UPDATE control_plane.schema_state
       SET version = 20260814000100,
           migrated_at = clock_timestamp()
     WHERE singleton = true;
END;
$$;
-- +goose StatementEnd

RESET ROLE;

-- +goose Down
-- Forward-only: old receipts cannot represent stable readiness identity.
SELECT 1;
