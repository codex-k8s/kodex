-- +goose Up
SET ROLE control_plane_owner;

CREATE TABLE control_plane.revision_impact_plans (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 ref text NOT NULL UNIQUE CHECK(ref ~ '^rvip_[A-Za-z0-9_-]{8,89}$'),
 organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
 actor_id uuid NOT NULL REFERENCES control_plane.subjects(id),
 kind text NOT NULL CHECK(kind IN ('RUNTIME_ENVIRONMENT','PROMPT_TEMPLATE','AGENT_INSTRUCTIONS')),
 version bigint NOT NULL DEFAULT 1 CHECK(version BETWEEN 1 AND 2),
 state text NOT NULL DEFAULT 'PREPARED' CHECK(state IN ('PREPARED','APPLIED')),
 snapshot jsonb NOT NULL CHECK(jsonb_typeof(snapshot)='object' AND octet_length(snapshot::text)<=16384),
 digest text NOT NULL CHECK(digest ~ '^[a-f0-9]{64}$'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 expires_at timestamptz NOT NULL DEFAULT clock_timestamp()+interval '15 minutes',
 published_revision_ref text NOT NULL DEFAULT '',
 applied_at timestamptz,
 CHECK(expires_at>created_at),
 CHECK((state='PREPARED' AND version=1 AND applied_at IS NULL AND published_revision_ref='') OR
       (state='APPLIED' AND version=2 AND applied_at IS NOT NULL AND published_revision_ref<>''))
);
CREATE INDEX revision_impact_actor ON control_plane.revision_impact_plans(organization_id,actor_id,ref);

CREATE TABLE control_plane.revision_impact_items (
 plan_id uuid NOT NULL REFERENCES control_plane.revision_impact_plans(id),
 ref text NOT NULL CHECK(ref ~ '^rvit_[A-Za-z0-9_-]{8,89}$'),
 snapshot jsonb NOT NULL CHECK(jsonb_typeof(snapshot)='object' AND octet_length(snapshot::text)<=16384),
 outcome text NOT NULL DEFAULT 'PENDING' CHECK(outcome IN ('PENDING','APPLIED','CONFLICT','FORBIDDEN','NOT_SELECTED')),
 result_revision_ref text NOT NULL DEFAULT '',
 result_binding_ref text NOT NULL DEFAULT '',
 result_binding_version bigint NOT NULL DEFAULT 0 CHECK(result_binding_version>=0),
 result_consumer_version bigint NOT NULL DEFAULT 0 CHECK(result_consumer_version>=0),
 PRIMARY KEY(plan_id,ref),
 CHECK((outcome='APPLIED' AND result_revision_ref<>'' AND result_consumer_version>0) OR
       (outcome<>'APPLIED' AND result_revision_ref='' AND result_binding_ref='' AND result_binding_version=0 AND result_consumer_version=0)),
 CHECK((result_binding_ref='' AND result_binding_version=0) OR (result_binding_ref<>'' AND result_binding_version>0))
);

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_revision_impact_plan() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR OLD.state<>'PREPARED' OR NEW.state<>'APPLIED' OR
    (to_jsonb(OLD)-ARRAY['state','version','applied_at','published_revision_ref']) IS DISTINCT FROM
    (to_jsonb(NEW)-ARRAY['state','version','applied_at','published_revision_ref']) OR NEW.version<>OLD.version+1 THEN
  RAISE EXCEPTION 'revision impact plan is immutable';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER revision_impact_plan_guard BEFORE UPDATE OR DELETE ON control_plane.revision_impact_plans
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_revision_impact_plan();

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_revision_impact_item() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE plan_state text;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'revision impact item is immutable'; END IF;
 SELECT state INTO plan_state FROM control_plane.revision_impact_plans WHERE id=NEW.plan_id FOR UPDATE;
 IF plan_state IS DISTINCT FROM 'PREPARED' THEN RAISE EXCEPTION 'revision impact plan is terminal'; END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.outcome<>'PENDING' OR (SELECT count(*) FROM control_plane.revision_impact_items WHERE plan_id=NEW.plan_id)>=1000 THEN
   RAISE EXCEPTION 'revision impact item limit is invalid';
  END IF;
 ELSIF OLD.outcome<>'PENDING' OR NEW.outcome='PENDING' OR OLD.plan_id<>NEW.plan_id OR
       OLD.ref<>NEW.ref OR OLD.snapshot IS DISTINCT FROM NEW.snapshot THEN
  RAISE EXCEPTION 'revision impact item is immutable';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER revision_impact_item_guard BEFORE INSERT OR UPDATE OR DELETE ON control_plane.revision_impact_items
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_revision_impact_item();
GRANT SELECT,INSERT,UPDATE ON control_plane.revision_impact_plans,control_plane.revision_impact_items TO control_plane_runtime;
RESET ROLE;
