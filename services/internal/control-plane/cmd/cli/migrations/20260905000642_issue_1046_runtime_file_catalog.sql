-- +goose Up
SET ROLE control_plane_owner;
-- Файловый grant хранит exact descriptors отдельно от bounded safe_snapshot.
CREATE TABLE control_plane.runtime_file_catalogs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^vfc_[A-Za-z0-9_-]{8,84}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid NOT NULL REFERENCES control_plane.projects(id),
    actor_id uuid NOT NULL REFERENCES control_plane.subjects(id),
    agent_id uuid NOT NULL REFERENCES control_plane.agents(id),
    node_id uuid NOT NULL REFERENCES control_plane.run_nodes(id),
    run_id uuid NOT NULL REFERENCES control_plane.runs(id),
    session_id uuid NOT NULL REFERENCES control_plane.sessions(id),
    turn_id uuid REFERENCES control_plane.session_turns(id),
    runtime_revision_ref text NOT NULL UNIQUE REFERENCES control_plane.runtime_revisions(ref)
        ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED,
    generation bigint NOT NULL CHECK (generation>0),
    purposes text[] NOT NULL CHECK (cardinality(purposes) BETWEEN 1 AND 4
        AND purposes <@ ARRAY['PROJECT','WORKSPACE_INPUT','RUN_RESULT','SKILL']::text[]),
    digest text CHECK (digest ~ '^[a-f0-9]{64}$'),
    total bigint NOT NULL DEFAULT 0 CHECK (total>=0),
    frozen boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    CHECK (NOT frozen OR digest IS NOT NULL)
);

CREATE TABLE control_plane.runtime_file_catalog_entries (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^vfe_[A-Za-z0-9_-]{8,84}$'),
    catalog_id uuid NOT NULL REFERENCES control_plane.runtime_file_catalogs(id) ON DELETE CASCADE,
    artifact_id uuid NOT NULL REFERENCES control_plane.artifacts(id),
    artifact_ref text NOT NULL,
    artifact_revision bigint NOT NULL CHECK (artifact_revision>0),
    artifact_version bigint NOT NULL CHECK (artifact_version>0),
    artifact_digest text NOT NULL CHECK (artifact_digest ~ '^sha256:[a-f0-9]{64}$'),
    file_name text NOT NULL CHECK (octet_length(file_name) BETWEEN 1 AND 1024),
    media_type text NOT NULL CHECK (octet_length(media_type) BETWEEN 1 AND 255),
    size_bytes bigint NOT NULL CHECK (size_bytes BETWEEN 0 AND 536870912),
    purpose text NOT NULL CHECK (purpose IN ('PROJECT','WORKSPACE_INPUT','RUN_RESULT','SKILL')),
    project_ref text NOT NULL,
    run_ref text NOT NULL DEFAULT '',
    source text NOT NULL,
    source_ref text NOT NULL DEFAULT '',
    source_revision_ref text NOT NULL DEFAULT '',
    entry_digest bytea NOT NULL CHECK (octet_length(entry_digest)=32),
    UNIQUE (catalog_id,purpose,artifact_ref,source_ref,source_revision_ref,file_name)
);
CREATE INDEX runtime_file_catalog_entries_page ON control_plane.runtime_file_catalog_entries(catalog_id,purpose,ref);

-- Единый предикат повторяется при materialization и каждом чтении каталога.
-- +goose StatementBegin
CREATE FUNCTION control_plane.runtime_file_source_visible(
    p_tenant uuid,p_actor uuid,p_project uuid,p_agent uuid,p_artifact uuid,p_purpose text,p_source_revision text
) RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER
SET search_path=pg_catalog,control_plane AS $$
    SELECT EXISTS (
        SELECT 1 FROM control_plane.catalog_access_targets file_target
        JOIN control_plane.artifacts file ON file.id=file_target.id AND file.organization_id=p_tenant
        JOIN control_plane.artifact_content content ON content.artifact_id=file.id
          AND content.digest=file.digest AND content.size_bytes=file.size_bytes
        JOIN control_plane.catalog_access_targets project_target
          ON project_target.organization_id=p_tenant AND project_target.kind='PROJECT' AND project_target.id=p_project
        LEFT JOIN control_plane.catalog_access_targets agent_target
          ON agent_target.organization_id=p_tenant AND agent_target.kind='AGENT' AND agent_target.id=p_agent
          AND agent_target.project_id=p_project
        JOIN control_plane.agents current_agent ON current_agent.id=p_agent AND current_agent.organization_id=p_tenant
        WHERE file_target.organization_id=p_tenant AND file_target.kind='ARTIFACT' AND file.id=p_artifact
          AND (file.project_id=p_project OR (p_purpose='WORKSPACE_INPUT' AND file.project_id IS NULL
              AND file.created_by=p_actor AND current_agent.system_key='system-assistant'))
          AND file.lifecycle_state='ACTIVE' AND file.scan_state='CLEAN'
          AND control_plane.catalog_resource_visible(p_tenant,p_actor,'project.view','PROJECT',project_target.id,
            project_target.project_id,project_target.owner_id,project_target.related_ids,statement_timestamp(),false)
          AND (control_plane.catalog_resource_visible(p_tenant,p_actor,'agent.view','AGENT',agent_target.id,
            agent_target.project_id,agent_target.owner_id,agent_target.related_ids,statement_timestamp(),false)
            OR (current_agent.system_key='system-assistant' AND current_agent.project_id IS NULL AND current_agent.state<>'ARCHIVED'))
          AND control_plane.catalog_resource_visible(p_tenant,p_actor,'artifact.view','ARTIFACT',file_target.id,
            file_target.project_id,file_target.owner_id,file_target.related_ids,statement_timestamp())
          AND control_plane.catalog_resource_visible(p_tenant,p_actor,'artifact.download','ARTIFACT',file_target.id,
            file_target.project_id,file_target.owner_id,file_target.related_ids,statement_timestamp())
          AND CASE p_purpose
            WHEN 'PROJECT' THEN file.run_id IS NULL AND 'platform.artifact.manage'=ANY(current_agent.capabilities)
            WHEN 'WORKSPACE_INPUT' THEN 'platform.artifact.manage'=ANY(current_agent.capabilities)
            WHEN 'RUN_RESULT' THEN file.source IN ('AGENT_RESULT','INTEGRATION_RESULT')
              AND 'platform.artifact.manage'=ANY(current_agent.capabilities)
              AND EXISTS (SELECT 1 FROM control_plane.catalog_access_targets run_target
                WHERE run_target.organization_id=p_tenant AND run_target.kind='RUN' AND run_target.id=file.run_id
                  AND control_plane.catalog_resource_visible(p_tenant,p_actor,'run.view','RUN',run_target.id,
                    run_target.project_id,run_target.owner_id,run_target.related_ids,statement_timestamp(),false))
            WHEN 'SKILL' THEN EXISTS (
              SELECT 1 FROM control_plane.agent_context_bindings binding
              JOIN control_plane.skill_bundle_revisions revision ON revision.id=binding.skill_revision_id
              JOIN control_plane.skill_bundles bundle ON bundle.id=revision.bundle_id AND bundle.id=binding.skill_bundle_id
              WHERE binding.organization_id=p_tenant AND binding.project_id=p_project AND binding.agent_id=p_agent AND binding.enabled
                AND revision.ref=p_source_revision AND bundle.project_id=p_project AND bundle.state='ACTIVE'
                AND control_plane.skill_revision_visible(p_tenant,p_actor,revision.id,statement_timestamp()))
            ELSE false END
    );
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.runtime_file_source_visible(uuid,uuid,uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.runtime_file_source_visible(uuid,uuid,uuid,uuid,uuid,text,text) TO control_plane_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_runtime_file_catalog() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME='runtime_file_catalog_entries' THEN
        IF TG_OP='INSERT' THEN
            IF NOT EXISTS (
                SELECT 1 FROM control_plane.runtime_file_catalogs catalog
                JOIN control_plane.artifacts artifact ON artifact.id=NEW.artifact_id
                  AND artifact.organization_id=catalog.organization_id
                  AND (artifact.project_id=catalog.project_id OR (NEW.purpose='WORKSPACE_INPUT'
                    AND artifact.project_id IS NULL AND artifact.created_by=catalog.actor_id))
                  AND artifact.ref=NEW.artifact_ref AND artifact.revision=NEW.artifact_revision
                  AND artifact.digest=NEW.artifact_digest AND artifact.size_bytes=NEW.size_bytes
                WHERE catalog.id=NEW.catalog_id AND NOT catalog.frozen AND NEW.purpose=ANY(catalog.purposes)
            ) THEN
                RAISE EXCEPTION 'runtime file catalog source is invalid';
            END IF;
            RETURN NEW;
        END IF;
        IF TG_OP='DELETE' AND NOT EXISTS (SELECT 1 FROM control_plane.runtime_file_catalogs WHERE id=OLD.catalog_id) THEN
            RETURN OLD;
        END IF;
        RAISE EXCEPTION 'runtime file catalog entry is immutable';
    END IF;
    IF TG_OP='DELETE' THEN
        IF NOT EXISTS (SELECT 1 FROM control_plane.runtime_revisions WHERE ref=OLD.runtime_revision_ref) THEN
            RETURN OLD;
        END IF;
        RAISE EXCEPTION 'runtime file catalog is immutable';
    END IF;
    IF OLD.frozen OR NOT NEW.frozen OR
       (to_jsonb(OLD)-ARRAY['frozen','total','digest']) IS DISTINCT FROM (to_jsonb(NEW)-ARRAY['frozen','total','digest']) THEN
        RAISE EXCEPTION 'runtime file catalog is immutable';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER protect_runtime_file_catalog BEFORE UPDATE OR DELETE ON control_plane.runtime_file_catalogs
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_runtime_file_catalog();
CREATE TRIGGER protect_runtime_file_catalog_entry BEFORE INSERT OR UPDATE OR DELETE ON control_plane.runtime_file_catalog_entries
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_runtime_file_catalog();

-- +goose StatementBegin
CREATE FUNCTION control_plane.verify_runtime_file_catalog_binding() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM control_plane.runtime_file_catalogs catalog
        JOIN control_plane.runtime_revisions revision ON revision.ref=catalog.runtime_revision_ref
          AND revision.organization_id=catalog.organization_id AND revision.project_id=catalog.project_id
          AND revision.agent_id=catalog.agent_id AND revision.node_id=catalog.node_id
          AND revision.run_id=catalog.run_id AND revision.session_id=catalog.session_id
          AND revision.turn_id IS NOT DISTINCT FROM catalog.turn_id AND revision.generation=catalog.generation
        JOIN control_plane.runs root ON root.id=revision.root_run_id AND root.initiated_by=catalog.actor_id
        WHERE catalog.id=NEW.id AND catalog.frozen
          AND revision.safe_snapshot #>> '{fileCatalog,ref}'=catalog.ref
          AND revision.safe_snapshot #>> '{fileCatalog,digest}'=catalog.digest
          AND revision.safe_snapshot #> '{fileCatalog,total}'=to_jsonb(catalog.total)
          AND revision.safe_snapshot #> '{fileCatalog,purposes}'=to_jsonb(catalog.purposes)
          AND catalog.total=(SELECT count(*) FROM control_plane.runtime_file_catalog_entries WHERE catalog_id=catalog.id)
    ) THEN
        RAISE EXCEPTION 'runtime file catalog execution binding is invalid';
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER verify_runtime_file_catalog_binding AFTER INSERT OR UPDATE ON control_plane.runtime_file_catalogs
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION control_plane.verify_runtime_file_catalog_binding();
CREATE VIEW control_plane.runtime_file_visible_entries WITH (security_invoker=true) AS
SELECT entry.id,entry.ref,entry.catalog_id,entry.artifact_id,entry.artifact_ref,entry.artifact_revision,
    entry.artifact_version,entry.artifact_digest,entry.file_name,entry.media_type,entry.size_bytes,
    entry.purpose,entry.project_ref,entry.run_ref,entry.source,entry.source_ref,entry.source_revision_ref,entry.entry_digest
FROM control_plane.runtime_file_catalog_entries entry
JOIN control_plane.runtime_file_catalogs catalog ON catalog.id=entry.catalog_id AND catalog.frozen
JOIN control_plane.runtime_revisions revision ON revision.ref=catalog.runtime_revision_ref
JOIN control_plane.artifacts artifact ON artifact.id=entry.artifact_id AND artifact.ref=entry.artifact_ref
  AND artifact.revision=entry.artifact_revision AND artifact.digest=entry.artifact_digest
  AND artifact.size_bytes=entry.size_bytes AND artifact.media_type=entry.media_type AND artifact.source=entry.source
  AND (entry.purpose='SKILL' OR artifact.file_name=entry.file_name)
WHERE control_plane.runtime_file_source_visible(catalog.organization_id,catalog.actor_id,catalog.project_id,
    catalog.agent_id,entry.artifact_id,entry.purpose,entry.source_revision_ref)
  AND (entry.purpose<>'SKILL' OR EXISTS (
    SELECT 1 FROM jsonb_array_elements(revision.safe_snapshot #> '{contextSnapshot,skills}') skill
    JOIN control_plane.agent_context_bindings binding
      ON binding.organization_id=catalog.organization_id AND binding.agent_id=catalog.agent_id
      AND binding.ref=skill->>'binding_ref' AND to_jsonb(binding.version)=skill->'binding_version' AND binding.enabled
    WHERE skill->>'bundle_ref'=entry.source_ref AND skill->>'revision_ref'=entry.source_revision_ref));
GRANT SELECT,INSERT,UPDATE ON control_plane.runtime_file_catalogs TO control_plane_runtime;
GRANT SELECT,INSERT ON control_plane.runtime_file_catalog_entries TO control_plane_runtime;
GRANT SELECT ON control_plane.runtime_file_visible_entries TO control_plane_runtime;
REVOKE ALL ON FUNCTION control_plane.protect_runtime_file_catalog() FROM PUBLIC;
REVOKE ALL ON FUNCTION control_plane.verify_runtime_file_catalog_binding() FROM PUBLIC;
RESET ROLE;

-- +goose Down
-- Forward-only: RuntimeRevision и audit остаются связанными с immutable grant.
SELECT 1;
