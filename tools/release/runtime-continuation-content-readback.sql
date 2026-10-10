-- #1797: exact owner-authorized read-only diagnosis; не command и не actor authority.
-- История, full snapshot, content и secret descriptors не выводятся.
BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout = '10s';
SET LOCAL lock_timeout = '1s';
WITH target AS MATERIALIZED (
    SELECT revision.id, revision.organization_id, revision.session_id,
           revision.turn_id, revision.node_id, revision.attempt,
           revision.safe_snapshot->>'continuationNoticeDigest' AS notice_digest,
           CASE WHEN jsonb_typeof(revision.safe_snapshot->'sessionContext') = 'array'
             THEN revision.safe_snapshot->'sessionContext'->-1 ELSE NULL END AS final_message
    FROM control_plane.runtime_revisions revision
    JOIN control_plane.organizations organization ON organization.id=revision.organization_id
    JOIN control_plane.runs run ON run.id=revision.run_id AND run.organization_id=revision.organization_id
    JOIN control_plane.sessions session ON session.id=revision.session_id AND session.organization_id=revision.organization_id
    JOIN control_plane.session_turns turn ON turn.id=revision.turn_id AND turn.session_id=revision.session_id
      AND turn.organization_id=revision.organization_id
    WHERE organization.ref=:'organization_ref' AND run.ref=:'run_ref'
      AND revision.ref=:'revision_ref' AND revision.generation=:'revision_version'::bigint
      AND revision.revision_digest=:'revision_digest'
      AND session.ref=:'session_ref' AND turn.ref=:'turn_ref' AND revision.attempt=:'attempt'::integer
), facts AS (
    SELECT CASE WHEN jsonb_typeof(target.final_message->'role')='string'
      AND target.final_message->>'role' IN ('USER','ASSISTANT','SYSTEM')
      THEN target.final_message->>'role' ELSE 'UNSUPPORTED' END AS final_role,
      CASE WHEN jsonb_typeof(target.final_message->'content')='string'
        THEN target.final_message->>'content' ELSE NULL END AS final_content,
      target.notice_digest, notice.count AS notice_count,
      COALESCE(notice.content_matches,false) AS notice_content_matches,
      COALESCE(notice.digest_matches,false) AS notice_digest_matches
    FROM target
    LEFT JOIN LATERAL (
      SELECT count(*) AS count,
        count(*)=1 AND bool_and(notice.content=target.final_message->>'content') AS content_matches,
        count(*)=1 AND bool_and(notice.materialization_digest=target.notice_digest
          AND notice.materialization_digest ~ '^[a-f0-9]{64}$') AS digest_matches
      FROM control_plane.session_continuation_notices notice
      WHERE notice.current_runtime_revision_id=target.id AND notice.organization_id=target.organization_id
        AND notice.session_id=target.session_id AND notice.turn_id=target.turn_id
        AND notice.node_id=target.node_id AND notice.attempt=target.attempt
    ) notice ON true
), document AS (
    SELECT jsonb_build_object(
      'version',1,'kind','RUNTIME_CONTINUATION_CONTENT_READBACK',
      'status',CASE WHEN EXISTS(SELECT 1 FROM target) THEN 'OBSERVED' ELSE 'NOT_FOUND' END,
      'organizationRef',:'organization_ref','runRef',:'run_ref','revisionRef',:'revision_ref',
      'revisionVersion',:'revision_version'::bigint,'sessionRef',:'session_ref','turnRef',:'turn_ref',
      'attempt',:'attempt'::integer,
      'role',(SELECT final_role FROM facts),
      'bytes',(SELECT octet_length(final_content) FROM facts),
      'codepoints',(SELECT char_length(final_content) FROM facts),
      -- Exact Go unicode.IsSpace/strings.TrimSpace, не locale-sensitive regexp.
      'trimEmpty',(SELECT char_length(btrim(final_content,
        U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000'))=0 FROM facts),
      -- PostgreSQL text/jsonb не может хранить NUL; literal \\u0000 не NUL.
      'hasNUL',CASE WHEN EXISTS(SELECT 1 FROM facts WHERE final_content IS NOT NULL) THEN false ELSE NULL END,
      'validUTF8',CASE WHEN EXISTS(SELECT 1 FROM facts WHERE final_content IS NOT NULL)
        THEN getdatabaseencoding()='UTF8' ELSE NULL END,
      'noticeCount',(SELECT notice_count FROM facts),
      'noticeContentMatches',(SELECT notice_content_matches FROM facts),
      'noticeDigestMatches',(SELECT notice_digest_matches FROM facts)
    ) AS body
)
SELECT CASE WHEN octet_length(body::text)<=8192 THEN body ELSE
  jsonb_build_object('version',1,'kind','RUNTIME_CONTINUATION_CONTENT_READBACK','status','BODY_LIMIT_EXCEEDED')
END::text FROM document;
COMMIT;

