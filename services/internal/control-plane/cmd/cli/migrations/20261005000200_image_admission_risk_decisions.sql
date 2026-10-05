-- +goose Up
SET ROLE control_plane_owner;

-- Старые verdict/evidence не дополняются придуманным отчётом или решением.
CREATE TABLE control_plane.image_vulnerability_reports (
    artifact_id uuid NOT NULL REFERENCES control_plane.image_artifacts(id) DEFERRABLE INITIALLY IMMEDIATE,
    admission_revision bigint NOT NULL CHECK (admission_revision > 0),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id) DEFERRABLE INITIALLY IMMEDIATE,
    projection_json text NOT NULL CHECK (octet_length(projection_json) BETWEEN 2 AND 4194304),
    projection_sha256 text NOT NULL CHECK (projection_sha256 ~ '^[a-f0-9]{64}$'),
    artifact_version bigint NOT NULL CHECK (artifact_version > 0),
    admission_receipt_sha256 text NOT NULL CHECK (admission_receipt_sha256 ~ '^[a-f0-9]{64}$'),
    evidence_manifest_digest text NOT NULL CHECK (evidence_manifest_digest ~ '^sha256:[a-f0-9]{64}$'),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (artifact_id, admission_revision)
);

CREATE TABLE control_plane.image_admission_risk_decisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^imgrisk_[A-Za-z0-9_-]{8,89}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id) DEFERRABLE INITIALLY IMMEDIATE,
    artifact_id uuid NOT NULL REFERENCES control_plane.image_artifacts(id) DEFERRABLE INITIALLY IMMEDIATE,
    admission_revision bigint NOT NULL CHECK (admission_revision > 0),
    actor_id uuid NOT NULL REFERENCES control_plane.subjects(id) DEFERRABLE INITIALLY IMMEDIATE,
    action text NOT NULL CHECK (action IN ('ACCEPT_RISK','REJECT_RISK')),
    reason text NOT NULL CHECK (octet_length(reason) BETWEEN 1 AND 2048 AND reason=btrim(reason) AND reason !~ '[[:cntrl:]]'),
    decision_json text NOT NULL CHECK (octet_length(decision_json) BETWEEN 2 AND 16384),
    decision_sha256 text NOT NULL CHECK (decision_sha256 ~ '^[a-f0-9]{64}$'),
    risk_acceptance_json text NOT NULL DEFAULT '' CHECK (octet_length(risk_acceptance_json) <= 16384),
    risk_acceptance_sha256 text NOT NULL DEFAULT '' CHECK (risk_acceptance_sha256='' OR risk_acceptance_sha256 ~ '^[a-f0-9]{64}$'),
    decided_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (artifact_id, admission_revision),
    FOREIGN KEY (artifact_id, admission_revision) REFERENCES control_plane.image_vulnerability_reports(artifact_id, admission_revision) DEFERRABLE INITIALLY IMMEDIATE,
    CHECK ((action='ACCEPT_RISK')=(risk_acceptance_json<>'' AND risk_acceptance_sha256<>''))
);

CREATE TABLE control_plane.image_admission_attempts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^imgadm_[A-Za-z0-9_-]{8,89}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id) DEFERRABLE INITIALLY IMMEDIATE,
    artifact_id uuid NOT NULL REFERENCES control_plane.image_artifacts(id) DEFERRABLE INITIALLY IMMEDIATE,
    number integer NOT NULL CHECK (number > 0),
    risk_decision_id uuid REFERENCES control_plane.image_admission_risk_decisions(id) DEFERRABLE INITIALLY IMMEDIATE,
    source_admission_revision bigint NOT NULL CHECK (source_admission_revision >= 0),
    source_receipt_sha256 text NOT NULL DEFAULT '',
    source_evidence_manifest_digest text NOT NULL DEFAULT '',
    source_artifact_json jsonb NOT NULL,
    state text NOT NULL CHECK (state IN ('PENDING','CLAIMED','ACCEPTED','REJECTED','FAILED','CANCELLED')),
    version bigint NOT NULL DEFAULT 1 CHECK (version>0),
    fence bigint NOT NULL DEFAULT 0 CHECK (fence>=0),
    terminal_artifact_json jsonb,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    finished_at timestamptz,
    UNIQUE (artifact_id, number),
    CHECK ((state IN ('ACCEPTED','REJECTED','FAILED','CANCELLED'))=(finished_at IS NOT NULL AND terminal_artifact_json IS NOT NULL))
);

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_image_risk_history() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $$
DECLARE artifact control_plane.image_artifacts%ROWTYPE;
BEGIN
    IF TG_OP='DELETE' THEN
      -- Только exact row target внутри существующей SECURITY DEFINER purge TX.
      IF current_user='control_plane_owner' AND EXISTS (
        SELECT 1 FROM control_plane.project_purge_context context
        JOIN control_plane.projects project ON project.id=context.project_id AND project.organization_id=OLD.organization_id
        JOIN control_plane.project_purge_receipts receipt ON receipt.project_id=project.id AND receipt.organization_id=project.organization_id
        JOIN control_plane.project_purge_targets target ON target.transaction_id=context.transaction_id AND target.relation_id=TG_RELID
        WHERE context.transaction_id=pg_current_xact_id_if_assigned() AND context.backend_pid=pg_backend_pid()
          AND project.lifecycle='PURGE_PENDING' AND receipt.state='OBJECTS_CLEARED' AND receipt.objects_cleared_at IS NOT NULL
          AND project.ref=CASE TG_TABLE_NAME WHEN 'image_vulnerability_reports' THEN (to_jsonb(OLD)->>'projection_json')::jsonb->>'projectRef'
            WHEN 'image_admission_risk_decisions' THEN (to_jsonb(OLD)->>'decision_json')::jsonb->>'ProjectRef'
            WHEN 'image_admission_attempts' THEN to_jsonb(OLD)->'source_artifact_json'->>'ProjectRef' END
          AND target.row_key=CASE TG_TABLE_NAME WHEN 'image_vulnerability_reports' THEN jsonb_build_array((to_jsonb(OLD)->>'artifact_id')::uuid,(to_jsonb(OLD)->>'admission_revision')::bigint)
            ELSE jsonb_build_array((to_jsonb(OLD)->>'id')::uuid) END) THEN RETURN OLD; END IF;
      RAISE EXCEPTION 'image risk history is immutable' USING ERRCODE='23514';
    END IF;
    IF TG_OP='UPDATE' THEN
        IF TG_TABLE_NAME<>'image_admission_attempts' OR OLD.state NOT IN ('PENDING','CLAIMED') OR
           NEW.state NOT IN ('CLAIMED','ACCEPTED','REJECTED','FAILED','CANCELLED') OR NEW.version<>OLD.version+1 OR NEW.fence<OLD.fence OR
           (to_jsonb(NEW)-ARRAY['state','terminal_artifact_json','finished_at','version','fence']) IS DISTINCT FROM
           (to_jsonb(OLD)-ARRAY['state','terminal_artifact_json','finished_at','version','fence']) THEN
            RAISE EXCEPTION 'image risk history is immutable' USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;
    SELECT * INTO artifact FROM control_plane.image_artifacts WHERE id=NEW.artifact_id FOR KEY SHARE;
    IF NOT FOUND OR artifact.organization_id<>NEW.organization_id THEN
        RAISE EXCEPTION 'image risk history owner mismatch' USING ERRCODE='23514';
    END IF;
    IF TG_TABLE_NAME='image_vulnerability_reports' THEN
        IF artifact.admission_revision<>NEW.admission_revision OR artifact.version<>NEW.artifact_version OR
           artifact.admission_receipt_sha256<>NEW.admission_receipt_sha256 OR
           artifact.admission_receipt_oci_manifest_digest<>NEW.evidence_manifest_digest OR
           encode(public.digest(NEW.projection_json,'sha256'),'hex')<>NEW.projection_sha256 OR
           (NEW.projection_json::jsonb->>'artifactRef') IS DISTINCT FROM artifact.ref OR
           (NEW.projection_json::jsonb->>'imageDigest') IS DISTINCT FROM artifact.manifest_digest OR
           (NEW.projection_json::jsonb->>'reportSHA256') IS DISTINCT FROM artifact.vulnerability_evidence_sha256 OR
           (NEW.projection_json::jsonb->>'sbomSHA256') IS DISTINCT FROM artifact.sbom_sha256 OR
           (NEW.projection_json::jsonb->>'scopeKind') IS DISTINCT FROM artifact.scope_kind OR
           (NEW.projection_json::jsonb->>'organizationRef') IS DISTINCT FROM (SELECT ref FROM control_plane.organizations WHERE id=artifact.organization_id) OR
           (NEW.projection_json::jsonb->>'projectRef') IS DISTINCT FROM COALESCE((SELECT ref FROM control_plane.projects WHERE id=artifact.project_id),'') OR
           (NEW.projection_json::jsonb->>'recipeRef') IS DISTINCT FROM (SELECT ref FROM control_plane.role_image_recipes WHERE id=artifact.recipe_id) OR
           (NEW.projection_json::jsonb->>'recipeVersion')::bigint IS DISTINCT FROM artifact.recipe_version OR
           (NEW.projection_json::jsonb->>'recipeGeneration')::bigint IS DISTINCT FROM artifact.recipe_generation OR
           (NEW.projection_json::jsonb->>'buildRef') IS DISTINCT FROM (SELECT ref FROM control_plane.image_builds WHERE id=artifact.build_id) OR
           (NEW.projection_json::jsonb->>'buildVersion')::bigint IS DISTINCT FROM artifact.build_version OR
           (NEW.projection_json::jsonb->>'buildAttempt')::integer IS DISTINCT FROM artifact.build_attempt OR
           (NEW.projection_json::jsonb->>'policyRevision')::bigint IS DISTINCT FROM artifact.policy_revision OR
           (NEW.projection_json::jsonb->>'policySHA256') IS DISTINCT FROM artifact.policy_sha256 THEN
            RAISE EXCEPTION 'image vulnerability report binding mismatch' USING ERRCODE='23514';
        END IF;
    ELSIF TG_TABLE_NAME='image_admission_risk_decisions' THEN
        IF artifact.admission_state<>'REJECTED' OR artifact.admission_revision<>NEW.admission_revision OR
           NOT EXISTS (SELECT 1 FROM control_plane.subjects subject WHERE subject.id=NEW.actor_id
             AND subject.organization_id=NEW.organization_id AND subject.active AND subject.kind='USER') OR
           NOT EXISTS (SELECT 1 FROM control_plane.access_bindings binding
             JOIN control_plane.application_role_versions version ON version.id=binding.role_version_id
             JOIN control_plane.application_roles role ON role.id=version.role_id
             WHERE binding.organization_id=NEW.organization_id AND binding.subject_id=NEW.actor_id
               AND binding.subject_kind='USER' AND binding.state='ACTIVE' AND binding.scope_kind='ORGANIZATION'
               AND role.kind='SYSTEM' AND role.stable_key IN ('OWNER','ADMINISTRATOR')
               AND (binding.valid_from IS NULL OR binding.valid_from<=clock_timestamp())
               AND (binding.valid_until IS NULL OR binding.valid_until>clock_timestamp())) OR
           NOT control_plane.catalog_resource_visible(NEW.organization_id,NEW.actor_id,'organization.manage','ORGANIZATION',NEW.organization_id,NULL,NULL,'{}'::jsonb,transaction_timestamp()) OR
           (NEW.decision_json::jsonb->>'ArtifactRef') IS DISTINCT FROM artifact.ref OR
           (NEW.decision_json::jsonb->>'ArtifactVersion')::bigint IS DISTINCT FROM artifact.version OR
           (NEW.decision_json::jsonb->>'ManifestDigest') IS DISTINCT FROM artifact.manifest_digest OR
           (NEW.decision_json::jsonb->>'VulnerabilityEvidenceSHA256') IS DISTINCT FROM artifact.vulnerability_evidence_sha256 OR
           (NEW.decision_json::jsonb->>'PriorAdmissionReceiptSHA256') IS DISTINCT FROM artifact.admission_receipt_sha256 OR
           (NEW.decision_json::jsonb->>'PriorEvidenceManifestDigest') IS DISTINCT FROM artifact.admission_receipt_oci_manifest_digest OR
           (NEW.decision_json::jsonb->>'Ref') IS DISTINCT FROM NEW.ref OR
           (NEW.decision_json::jsonb->>'ActorRef') IS DISTINCT FROM (SELECT ref FROM control_plane.subjects WHERE id=NEW.actor_id) OR
           (NEW.decision_json::jsonb->>'Action') IS DISTINCT FROM NEW.action OR
           (NEW.decision_json::jsonb->>'Reason') IS DISTINCT FROM NEW.reason OR
           (NEW.decision_json::jsonb->>'AdmissionRevision')::bigint IS DISTINCT FROM artifact.admission_revision OR
           (NEW.decision_json::jsonb->>'ScopeKind') IS DISTINCT FROM artifact.scope_kind OR
           (NEW.decision_json::jsonb->>'OrganizationRef') IS DISTINCT FROM (SELECT ref FROM control_plane.organizations WHERE id=artifact.organization_id) OR
           (NEW.decision_json::jsonb->>'ProjectRef') IS DISTINCT FROM COALESCE((SELECT ref FROM control_plane.projects WHERE id=artifact.project_id),'') OR
           (NEW.decision_json::jsonb->>'ProjectionSHA256') IS DISTINCT FROM (SELECT projection_sha256 FROM control_plane.image_vulnerability_reports WHERE artifact_id=artifact.id AND admission_revision=NEW.admission_revision) OR
           (NEW.decision_json::jsonb->>'RecipeVersion')::bigint IS DISTINCT FROM artifact.recipe_version OR
           (NEW.decision_json::jsonb->>'RecipeGeneration')::bigint IS DISTINCT FROM artifact.recipe_generation OR
           (NEW.decision_json::jsonb->>'BuildRef') IS DISTINCT FROM (SELECT ref FROM control_plane.image_builds WHERE id=artifact.build_id) OR
           (NEW.decision_json::jsonb->>'BuildAttempt')::integer IS DISTINCT FROM artifact.build_attempt OR
           (NEW.decision_json::jsonb->>'PolicyRevision')::bigint IS DISTINCT FROM artifact.policy_revision OR
           (NEW.decision_json::jsonb->>'PolicySHA256') IS DISTINCT FROM artifact.policy_sha256 OR
           encode(public.digest(NEW.decision_json,'sha256'),'hex')<>NEW.decision_sha256 OR
           (NEW.action='ACCEPT_RISK' AND encode(public.digest(NEW.risk_acceptance_json,'sha256'),'hex')<>NEW.risk_acceptance_sha256) THEN
            RAISE EXCEPTION 'image risk decision authority mismatch' USING ERRCODE='23514';
        END IF;
    ELSIF TG_TABLE_NAME='image_admission_attempts' THEN
      IF NEW.number<>(SELECT COALESCE(max(number),0)+1 FROM control_plane.image_admission_attempts WHERE artifact_id=NEW.artifact_id) OR
         NEW.source_admission_revision<>artifact.admission_revision OR NEW.source_receipt_sha256<>artifact.admission_receipt_sha256 OR
         NEW.source_evidence_manifest_digest<>artifact.admission_receipt_oci_manifest_digest OR
         (NEW.source_artifact_json->>'Ref') IS DISTINCT FROM artifact.ref OR
         (NEW.source_artifact_json->>'Version')::bigint IS DISTINCT FROM artifact.version OR
         (NEW.source_artifact_json->>'OrganizationRef') IS DISTINCT FROM (SELECT ref FROM control_plane.organizations WHERE id=artifact.organization_id) OR
         (NEW.source_artifact_json->>'ProjectRef') IS DISTINCT FROM COALESCE((SELECT ref FROM control_plane.projects WHERE id=artifact.project_id),'') OR
         (NEW.source_artifact_json->>'ScopeKind') IS DISTINCT FROM artifact.scope_kind THEN
        RAISE EXCEPTION 'image admission attempt source mismatch' USING ERRCODE='23514';END IF;
      IF NEW.risk_decision_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM control_plane.image_admission_risk_decisions decision
        WHERE decision.id=NEW.risk_decision_id AND decision.artifact_id=NEW.artifact_id
          AND decision.organization_id=NEW.organization_id AND decision.action='ACCEPT_RISK'
          AND decision.admission_revision=NEW.source_admission_revision) THEN
        RAISE EXCEPTION 'image admission risk attempt mismatch' USING ERRCODE='23514';
      END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER image_vulnerability_report_immutable BEFORE INSERT OR UPDATE OR DELETE ON control_plane.image_vulnerability_reports FOR EACH ROW EXECUTE FUNCTION control_plane.guard_image_risk_history();
CREATE TRIGGER image_admission_risk_decision_immutable BEFORE INSERT OR UPDATE OR DELETE ON control_plane.image_admission_risk_decisions FOR EACH ROW EXECUTE FUNCTION control_plane.guard_image_risk_history();
CREATE TRIGGER image_admission_attempt_immutable BEFORE INSERT OR UPDATE OR DELETE ON control_plane.image_admission_attempts FOR EACH ROW EXECUTE FUNCTION control_plane.guard_image_risk_history();
-- +goose StatementBegin
CREATE FUNCTION control_plane.finish_image_admission_attempt() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $$
BEGIN
    IF OLD.admission_state IN ('PENDING','CLAIMED') AND NEW.admission_state IN ('ACCEPTED','REJECTED','FAILED') THEN
        UPDATE control_plane.image_admission_attempts SET
          state=CASE WHEN NEW.admission_state='REJECTED' AND NEW.admission_verdict='' THEN 'CANCELLED' ELSE NEW.admission_state END,
          terminal_artifact_json=to_jsonb(NEW),finished_at=clock_timestamp(),version=version+1,fence=NEW.admission_fence
        WHERE artifact_id=NEW.id AND state IN ('PENDING','CLAIMED');
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER finish_image_admission_attempt AFTER UPDATE ON control_plane.image_artifacts FOR EACH ROW EXECUTE FUNCTION control_plane.finish_image_admission_attempt();
-- Обновление/архивирование recipe и новая build закрывают прежнюю активную attempt в owner TX.
-- +goose StatementBegin
CREATE FUNCTION control_plane.invalidate_image_admission_attempts() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $$
DECLARE target_recipe_id uuid;
BEGIN
    IF TG_TABLE_NAME='role_image_recipes' THEN
      IF ROW(OLD.version,OLD.generation,OLD.state) IS NOT DISTINCT FROM ROW(NEW.version,NEW.generation,NEW.state) THEN RETURN NEW; END IF;
      target_recipe_id:=NEW.id;
    ELSE target_recipe_id:=NEW.recipe_id;
    END IF;
    UPDATE control_plane.image_artifacts artifact SET admission_state='REJECTED',admission_verdict='',
      admission_fence=admission_fence+1,admission_claimant_workload=NULL,admission_authority_generation=0,
      admission_claim_token_sha256=NULL,admission_claim_expires_at=NULL,promotion_state='REJECTED',
      promotion_fence=promotion_fence+1,promotion_claimant_workload=NULL,promotion_authority_generation=0,
      promotion_claim_token_sha256=NULL,promotion_claim_expires_at=NULL,promotion_authorization_token_sha256=NULL,
      promotion_authorization_expires_at=NULL,version=version+1,updated_at=clock_timestamp()
    WHERE artifact.recipe_id=target_recipe_id AND artifact.admission_state IN ('PENDING','CLAIMED');
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER invalidate_recipe_image_admission AFTER UPDATE ON control_plane.role_image_recipes FOR EACH ROW EXECUTE FUNCTION control_plane.invalidate_image_admission_attempts();
CREATE TRIGGER invalidate_build_image_admission AFTER INSERT ON control_plane.image_builds FOR EACH ROW EXECUTE FUNCTION control_plane.invalidate_image_admission_attempts();
GRANT SELECT, INSERT ON control_plane.image_vulnerability_reports,control_plane.image_admission_risk_decisions TO control_plane_runtime;
GRANT SELECT, INSERT, UPDATE ON control_plane.image_admission_attempts TO control_plane_runtime;

-- Exact forward-only обновление известного FK-графа, без wildcard purge.
-- +goose StatementBegin
DO $migration$
DECLARE
 v_nodes integer;v_node_hash text;v_edges integer;v_edge_hash text;v_definition text;
 v_old_nodes constant text := 'IF v_nodes <> 97 OR v_node_fingerprint <> ''02b472590335b8fb7370804b386cdf32'' THEN';
 v_new_nodes constant text := 'IF v_nodes <> 100 OR v_node_fingerprint <> ''41669910a543578b4ef955aefbb8a25a'' THEN';
 v_old_edges constant text := 'IF v_edges <> 248 OR v_edge_fingerprint <> ''1d12e5e5a6c4475ccc79b868af564679'' THEN';
 v_new_edges constant text := 'IF v_edges <> 253 OR v_edge_fingerprint <> ''372b45622a9bc15b0f10dcc5335b9883'' THEN';
BEGIN
 WITH RECURSIVE nodes(relation_id) AS (SELECT 'control_plane.projects'::regclass::oid UNION
  SELECT c.conrelid FROM nodes JOIN pg_constraint c ON c.contype='f' AND c.confrelid=nodes.relation_id
  JOIN pg_namespace n ON n.oid=c.connamespace AND n.nspname='control_plane')
 SELECT count(*),md5(string_agg(n.nspname||'.'||c.relname,E'\n' ORDER BY n.nspname,c.relname))
 INTO v_nodes,v_node_hash FROM nodes JOIN pg_class c ON c.oid=nodes.relation_id JOIN pg_namespace n ON n.oid=c.relnamespace;
 WITH RECURSIVE nodes(relation_id) AS (SELECT 'control_plane.projects'::regclass::oid UNION
  SELECT c.conrelid FROM nodes JOIN pg_constraint c ON c.contype='f' AND c.confrelid=nodes.relation_id
  JOIN pg_namespace n ON n.oid=c.connamespace AND n.nspname='control_plane')
 SELECT count(*),md5(string_agg(cn.nspname||'.'||cc.relname||'|'||c.conname||'|'||pn.nspname||'.'||pc.relname||'|'||c.confdeltype::text,
 E'\n' ORDER BY cn.nspname,cc.relname,c.conname)) INTO v_edges,v_edge_hash
 FROM pg_constraint c JOIN pg_class cc ON cc.oid=c.conrelid JOIN pg_namespace cn ON cn.oid=cc.relnamespace
 JOIN pg_class pc ON pc.oid=c.confrelid JOIN pg_namespace pn ON pn.oid=pc.relnamespace
 WHERE c.contype='f' AND c.confrelid IN (SELECT relation_id FROM nodes) AND c.conrelid IN (SELECT relation_id FROM nodes);
 IF v_nodes<>100 OR v_node_hash<>'41669910a543578b4ef955aefbb8a25a' OR v_edges<>253 OR v_edge_hash<>'372b45622a9bc15b0f10dcc5335b9883' THEN
  RAISE EXCEPTION 'image risk purge graph migration precondition failed';END IF;
 SELECT pg_get_functiondef('control_plane.purge_project_database(uuid,uuid,text)'::regprocedure) INTO v_definition;
 IF strpos(v_definition,v_old_nodes)=0 OR strpos(v_definition,v_old_edges)=0 OR
 strpos(substr(v_definition,strpos(v_definition,v_old_nodes)+length(v_old_nodes)),v_old_nodes)>0 OR
 strpos(substr(v_definition,strpos(v_definition,v_old_edges)+length(v_old_edges)),v_old_edges)>0 THEN
  RAISE EXCEPTION 'image risk purge function migration precondition failed';END IF;
 EXECUTE replace(replace(v_definition,v_old_nodes,v_new_nodes),v_old_edges,v_new_edges);
END;
$migration$;
-- +goose StatementEnd
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'image admission risk decisions migration is forward-only'; END $$;
-- +goose StatementEnd
