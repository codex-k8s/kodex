-- Только maintenance с очередью, без выполняющихся turn. Значения Secret не читаются.
BEGIN READ ONLY;
SET LOCAL statement_timeout = '5s';
SELECT jsonb_build_object('at',clock_timestamp(),
    'runningNodes',(SELECT count(*) FROM control_plane.run_nodes WHERE type='AGENT_EXECUTION' AND state='RUNNING'),
    'claimedLeases',(SELECT count(*) FROM control_plane.runtime_leases WHERE state='CLAIMED'),
    'openBuilds',(SELECT count(*) FROM control_plane.image_builds WHERE stage NOT IN ('COMPLETED','FAILED','CANCELLED','EXPIRED','DEAD_LETTER')),
    'pendingAdmissions',(SELECT count(*) FROM control_plane.image_artifacts WHERE admission_state IN ('PENDING','CLAIMED')),
    'pendingPromotions',(SELECT count(*) FROM control_plane.role_image_promotion_requests WHERE state IN ('QUEUED','PROMOTING')) +
        (SELECT count(*) FROM control_plane.image_artifacts WHERE promotion_state IN ('CLAIMED','AUTHORIZED')),
    'pins',(SELECT encode(sha256(convert_to(COALESCE(string_agg(concat_ws(':',id::text,manifest_digest,promoted_reference,policy_sha256,version::text),E'\n' ORDER BY id),''),'UTF8')),'hex') FROM control_plane.image_artifacts WHERE promotion_state='PROMOTED'));
COMMIT;
