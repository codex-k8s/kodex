-- Только disposable fixture: immutable архив общей сессии до переноса в проект.
WITH selected AS (
    SELECT session.id,session.organization_id,session.project_id,session.provider_account_id,
        session.ref,organization.ref organization_ref,revision.id revision_id,
        storage.native_session_id,storage.native_checkpoint,storage.content_generation,
        storage.source_relative_path,storage.source_sha256,storage.source_size_bytes
    FROM control_plane.sessions session
    JOIN control_plane.organizations organization ON organization.id=session.organization_id
    JOIN control_plane.runtime_revisions revision ON revision.session_id=session.id
    JOIN control_plane.session_storage storage ON storage.session_id=session.id AND storage.runtime_revision_id=revision.id
    WHERE session.ref=$1
    ORDER BY revision.generation DESC LIMIT 1
), archived AS (
    INSERT INTO control_plane.session_archives
        (ref,organization_id,project_id,session_id,provider_account_id,runtime_revision_id,
         native_session_id,native_checkpoint,content_generation,format_version,source_relative_path,source_sha256,source_size_bytes,
         object_key,object_version,object_etag,object_digest,object_size_bytes,retention_until)
    SELECT 'sar_purge_moved_fixture',organization_id,project_id,id,provider_account_id,revision_id,
        native_session_id,native_checkpoint,content_generation,3,
        source_relative_path,source_sha256,source_size_bytes,
        'session-archive/v3/' || organization_ref || '/_system/' || ref || '/g' || content_generation || '/sat_purge_moved_fixture-a1.tar',
        'fixture-version-1','fixture-etag','sha256:' || repeat('b',64),1024,clock_timestamp()+interval '1 day'
    FROM selected
    RETURNING id,organization_id,project_id,session_id,object_key,object_version,content_generation
), task AS (
    INSERT INTO control_plane.session_archive_tasks
        (ref,organization_id,project_id,session_id,archive_id,kind,state,content_generation,input_digest,object_key,object_version,completed_at)
    SELECT 'sat_purge_moved_fixture',organization_id,project_id,session_id,id,'SNAPSHOT','SUCCEEDED',content_generation,repeat('c',64),object_key,object_version,clock_timestamp()
    FROM archived
)
SELECT object_key FROM archived;
