-- Только disposable fixture: immutable архив общей сессии до переноса в проект.
WITH selected AS (
    SELECT session.id,session.organization_id,session.project_id,session.provider_account_id,
        session.ref,organization.ref organization_ref,revision.id revision_id
    FROM control_plane.sessions session
    JOIN control_plane.organizations organization ON organization.id=session.organization_id
    JOIN control_plane.runtime_revisions revision ON revision.session_id=session.id
    WHERE session.ref=$1
    ORDER BY revision.generation DESC LIMIT 1
), archived AS (
    INSERT INTO control_plane.session_archives
        (ref,organization_id,project_id,session_id,provider_account_id,runtime_revision_id,
         codex_session_id,content_generation,format_version,source_relative_path,source_sha256,source_size_bytes,
         object_key,object_version,object_etag,object_digest,object_size_bytes,retention_until)
    SELECT 'sar_purge_moved_fixture',organization_id,project_id,id,provider_account_id,revision_id,
        '20000000-0000-4000-8000-000000000003'::uuid,1,1,
        '.kodex/state/codex-home/sessions/2026/10/16/rollout-20000000-0000-4000-8000-000000000003.jsonl',repeat('a',64),128,
        'session-archive/v1/' || organization_ref || '/_system/' || ref || '/g1/sat_purge_moved_fixture-a1.tar',
        'fixture-version-1','fixture-etag','sha256:' || repeat('b',64),1024,clock_timestamp()+interval '1 day'
    FROM selected
    RETURNING id,organization_id,project_id,session_id,object_key,object_version
), task AS (
    INSERT INTO control_plane.session_archive_tasks
        (ref,organization_id,project_id,session_id,archive_id,kind,state,content_generation,input_digest,object_key,object_version,completed_at)
    SELECT 'sat_purge_moved_fixture',organization_id,project_id,session_id,id,'SNAPSHOT','SUCCEEDED',1,repeat('c',64),object_key,object_version,clock_timestamp()
    FROM archived
)
SELECT object_key FROM archived;
