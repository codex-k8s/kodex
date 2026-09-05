-- +goose Up
SET ROLE control_plane_owner;
CREATE TABLE control_plane.runtime_secret_draft_impact_plans (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK(ref ~ '^sdip_[A-Za-z0-9_-]{8,89}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    actor_id uuid NOT NULL REFERENCES control_plane.subjects(id),
    draft_id uuid NOT NULL REFERENCES control_plane.runtime_secret_drafts(id),
    draft_version bigint NOT NULL CHECK(draft_version>0),
    secret_version bigint NOT NULL CHECK(secret_version>0),
    source_revision bigint NOT NULL CHECK(source_revision>=0),
    credential_revision bigint NOT NULL CHECK(credential_revision>0),
    digest text NOT NULL CHECK(digest ~ '^[a-f0-9]{64}$'),
    state text NOT NULL DEFAULT 'PREPARED' CHECK(state IN ('PREPARED','APPLIED','CANCELLED')),
    operation_id uuid UNIQUE REFERENCES control_plane.runtime_secret_draft_operations(id),
    idempotency_key text NOT NULL,
    intent_digest text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL DEFAULT clock_timestamp()+interval '5 minutes',
    UNIQUE(organization_id,actor_id,idempotency_key)
);
CREATE TABLE control_plane.runtime_secret_draft_impact_items (
    ref text PRIMARY KEY CHECK(ref ~ '^sdit_[A-Za-z0-9_-]{8,89}$'),
    plan_id uuid NOT NULL REFERENCES control_plane.runtime_secret_draft_impact_plans(id),
    snapshot jsonb NOT NULL CHECK(jsonb_typeof(snapshot)='object'),
    outcome text NOT NULL DEFAULT 'PENDING' CHECK(outcome IN ('PENDING','APPLIED','CONFLICT','FORBIDDEN','NOT_SELECTED')),
    result_environment_version_ref text NOT NULL DEFAULT '',
    result_binding_ref text NOT NULL DEFAULT '',
    result_binding_version bigint NOT NULL DEFAULT 0
);
CREATE INDEX runtime_secret_draft_impact_items_plan ON control_plane.runtime_secret_draft_impact_items(plan_id,ref);
GRANT SELECT,INSERT,UPDATE ON control_plane.runtime_secret_draft_impact_plans,control_plane.runtime_secret_draft_impact_items TO control_plane_runtime;
RESET ROLE;
