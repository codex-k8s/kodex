-- +goose Up
SET ROLE control_plane_owner;

CREATE TABLE control_plane.agent_instruction_bindings (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 ref text NOT NULL UNIQUE CHECK(ref ~ '^inb_[A-Za-z0-9_-]{8,89}$'),
 organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
 agent_id uuid NOT NULL UNIQUE REFERENCES control_plane.agents(id),
 instruction_id uuid NOT NULL REFERENCES control_plane.instruction_versions(id),
 version bigint NOT NULL DEFAULT 1 CHECK(version BETWEEN 1 AND 9007199254740991),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

-- Backfill фиксирует фактически выбранный fallback, не выдумывает историю.
INSERT INTO control_plane.agent_instruction_bindings(ref,organization_id,agent_id,instruction_id)
SELECT 'inb_'||replace(gen_random_uuid()::text,'-',''),a.organization_id,a.id,i.id
FROM control_plane.agents a
JOIN LATERAL (
 SELECT v.id FROM control_plane.instruction_versions v
 WHERE v.organization_id=a.organization_id AND v.agent_id=a.id AND v.state='PUBLISHED'
 ORDER BY v.version_number DESC LIMIT 1
) i ON true;

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_agent_instruction_binding() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'instruction binding cannot be deleted'; END IF;
 IF TG_OP='INSERT' AND NEW.version<>1 THEN RAISE EXCEPTION 'instruction binding initial version is invalid'; END IF;
 IF NOT EXISTS(SELECT 1 FROM control_plane.agents a
   JOIN control_plane.instruction_versions i ON i.agent_id=a.id AND i.organization_id=a.organization_id
   WHERE a.id=NEW.agent_id AND a.organization_id=NEW.organization_id AND i.id=NEW.instruction_id
     AND i.state='PUBLISHED' AND i.published_at IS NOT NULL) THEN
  RAISE EXCEPTION 'instruction binding target is invalid';
 END IF;
 IF TG_OP='UPDATE' AND (NEW.ref<>OLD.ref OR NEW.id<>OLD.id OR
   NEW.organization_id<>OLD.organization_id OR NEW.agent_id<>OLD.agent_id OR NEW.version<>OLD.version+1) THEN
  RAISE EXCEPTION 'instruction binding identity is immutable';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER agent_instruction_binding_guard BEFORE INSERT OR UPDATE OR DELETE
 ON control_plane.agent_instruction_bindings FOR EACH ROW EXECUTE FUNCTION control_plane.guard_agent_instruction_binding();
GRANT SELECT,INSERT,UPDATE ON control_plane.agent_instruction_bindings TO control_plane_runtime;
RESET ROLE;
