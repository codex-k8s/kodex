-- +goose Up
SET ROLE control_plane_owner;

-- Наличие исторического format-only CLEAN не является AV-доказательством.
-- Новые поля принадлежат тому же immutable exact object receipt, без нового
-- графа FK или конкурирующего владельца состояния.
ALTER TABLE control_plane.artifact_revision_content ADD COLUMN antivirus_report jsonb;
ALTER TABLE control_plane.prepared_content ADD COLUMN antivirus_report jsonb;

-- +goose StatementBegin
CREATE FUNCTION control_plane.artifact_scan_receipt_valid(report jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE SET search_path=pg_catalog AS $$
 SELECT COALESCE(jsonb_typeof(report)='object'
   AND report ?& ARRAY['engine','policy','scannedAt','verdict']
   AND report-ARRAY['engine','policy','scannedAt','verdict']='{}'::jsonb
   AND report->>'engine' ~ '^ClamAV [^\r\n]{1,249}$'
   AND report->>'policy'='clamav-instream-v1'
   AND report->>'scannedAt' ~ '^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,9})?Z$'
   AND report->>'verdict' IN ('CLEAN','QUARANTINED'),false);
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.artifact_scan_receipt_valid(jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.artifact_scan_receipt_valid(jsonb) TO control_plane_runtime,artifact_retention_runtime;

ALTER TABLE control_plane.artifact_revision_content ADD CONSTRAINT artifact_antivirus_receipt_shape
 CHECK (antivirus_report IS NULL OR control_plane.artifact_scan_receipt_valid(antivirus_report));
ALTER TABLE control_plane.prepared_content ADD CONSTRAINT prepared_antivirus_receipt_shape
 CHECK (antivirus_report IS NULL OR control_plane.artifact_scan_receipt_valid(antivirus_report));

-- +goose StatementBegin
CREATE FUNCTION control_plane.require_artifact_antivirus_receipt() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF NOT control_plane.artifact_scan_receipt_valid(NEW.antivirus_report) THEN
   RAISE EXCEPTION 'artifact antivirus receipt is required';
 END IF;
 IF TG_TABLE_NAME='artifact_revision_content' THEN
 IF EXISTS (
   SELECT 1 FROM control_plane.artifact_revisions revision
   WHERE revision.id=NEW.revision_id AND revision.scan_state='CLEAN'
     AND NEW.antivirus_report->>'verdict'<>'CLEAN'
 ) THEN RAISE EXCEPTION 'artifact antivirus verdict binding is invalid'; END IF;
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER require_artifact_antivirus_receipt BEFORE INSERT ON control_plane.artifact_revision_content
 FOR EACH ROW EXECUTE FUNCTION control_plane.require_artifact_antivirus_receipt();
CREATE TRIGGER require_prepared_antivirus_receipt BEFORE INSERT ON control_plane.prepared_content
 FOR EACH ROW EXECUTE FUNCTION control_plane.require_artifact_antivirus_receipt();

-- Тот же источник eligibility для UI/history/runtime/binding/download.
-- Старые строки сохранены, но без exact AV proof больше не выдают CLEAN.
-- +goose StatementBegin
CREATE FUNCTION control_plane.artifact_effective_scan(p_revision_id uuid) RETURNS text
LANGUAGE sql STABLE PARALLEL SAFE SET search_path=pg_catalog AS $$
 SELECT CASE WHEN revision.scan_state='CLEAN' THEN
   CASE WHEN control_plane.artifact_scan_receipt_valid(content.antivirus_report)
     AND content.antivirus_report->>'verdict'='CLEAN'
     AND content.digest=revision.digest AND content.size_bytes=revision.size_bytes
     THEN 'CLEAN' ELSE 'QUARANTINED' END
   ELSE revision.scan_state END
 FROM control_plane.artifact_revisions revision
 LEFT JOIN control_plane.artifact_revision_content content ON content.revision_id=revision.id
 WHERE revision.id=p_revision_id;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.artifact_effective_scan(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.artifact_effective_scan(uuid) TO control_plane_runtime,artifact_retention_runtime;

CREATE OR REPLACE VIEW control_plane.artifact_history WITH (security_invoker=true) AS
SELECT head.id,head.ref,head.organization_id,head.project_id,head.run_id,head.node_id,
       revision.file_name,revision.media_type,revision.size_bytes,revision.digest,
       revision.source,control_plane.artifact_effective_scan(revision.id) AS scan_state,
       revision.object_receipt_ref,
       CASE WHEN control_plane.artifact_effective_scan(revision.id)='CLEAN' THEN revision.preview_state ELSE 'BLOCKED' END AS preview_state,
       revision.revision,head.version,head.created_by,revision.created_at,
       head.lifecycle_state,head.deleted_at,head.purge_after,head.purged_at,
       head.retention_claim_owner,head.retention_claim_generation,head.retention_claim_expires_at,
       revision.id AS revision_id,revision.ref AS revision_ref
FROM control_plane.artifact_heads head
JOIN control_plane.artifact_revisions revision ON revision.artifact_id=head.id;

CREATE OR REPLACE VIEW control_plane.artifacts WITH (security_invoker=true) AS
SELECT head.id,head.ref,head.organization_id,head.project_id,head.run_id,head.node_id,
       CASE WHEN head.lifecycle_state='PURGED' THEN 'purged-' || head.ref ELSE revision.file_name END AS file_name,
       CASE WHEN head.lifecycle_state='PURGED' THEN 'application/octet-stream' ELSE revision.media_type END AS media_type,
       CASE WHEN head.lifecycle_state='PURGED' THEN 0::bigint ELSE revision.size_bytes END AS size_bytes,
       CASE WHEN head.lifecycle_state='PURGED' THEN 'sha256:' || repeat('0',64) ELSE revision.digest END AS digest,
       CASE WHEN head.lifecycle_state='PURGED' THEN 'CONTROL_CENTER' ELSE revision.source END AS source,
       CASE WHEN head.lifecycle_state='PURGED' THEN 'FAILED' ELSE control_plane.artifact_effective_scan(revision.id) END AS scan_state,
       CASE WHEN head.lifecycle_state='PURGED' THEN '' ELSE revision.object_receipt_ref END AS object_receipt_ref,
       CASE WHEN head.lifecycle_state='PURGED' OR control_plane.artifact_effective_scan(revision.id) IS DISTINCT FROM 'CLEAN'
         THEN 'BLOCKED' ELSE revision.preview_state END AS preview_state,
       CASE WHEN head.lifecycle_state='PURGED' THEN 1::bigint ELSE revision.revision END AS revision,
       head.version,head.created_by,head.created_at,
       head.lifecycle_state,head.deleted_at,head.purge_after,head.purged_at,
       head.retention_claim_owner,head.retention_claim_generation,head.retention_claim_expires_at,
       revision.id AS revision_id,COALESCE(revision.ref,'') AS current_revision_ref
FROM control_plane.artifact_heads head
LEFT JOIN control_plane.artifact_revisions revision ON revision.id=head.current_revision_id AND revision.artifact_id=head.id;

CREATE OR REPLACE VIEW control_plane.artifact_content WITH (security_invoker=true) AS
SELECT head.id AS artifact_id,content.object_key,content.object_version,content.object_etag,
       content.digest,content.size_bytes,content.stored_at,content.antivirus_report
FROM control_plane.artifact_heads head
JOIN control_plane.artifact_revision_content content ON content.revision_id=head.current_revision_id;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.insert_artifact_content_projection() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE selected_revision uuid;
BEGIN
    SELECT revision.id INTO STRICT selected_revision
    FROM control_plane.artifact_heads head
    JOIN control_plane.artifact_revisions revision ON revision.id=head.current_revision_id
    WHERE head.id=NEW.artifact_id AND head.lifecycle_state='ACTIVE'
      AND revision.digest=NEW.digest AND revision.size_bytes=NEW.size_bytes
    FOR UPDATE OF head;
    INSERT INTO control_plane.artifact_revision_content
        (revision_id,object_key,object_version,object_etag,digest,size_bytes,stored_at,antivirus_report)
    VALUES (selected_revision,NEW.object_key,NEW.object_version,NEW.object_etag,
            NEW.digest,NEW.size_bytes,coalesce(NEW.stored_at,clock_timestamp()),NEW.antivirus_report);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
RESET ROLE;

-- +goose Down
-- Forward-only: история остаётся неизменной, AV eligibility не ослабляется.
SELECT 1;
