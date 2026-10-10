BEGIN;
SET LOCAL ROLE control_plane_owner;
DO $$ BEGIN
 IF (SELECT count(*) FROM control_plane.artifact_heads WHERE ref LIKE 'art_upgrade_%')<>2 OR
    (SELECT count(*) FROM control_plane.artifact_revisions WHERE artifact_id IN(SELECT id FROM control_plane.artifact_heads WHERE ref LIKE 'art_upgrade_%'))<>2 OR
    (SELECT to_jsonb('control_plane.artifact_heads'::regclass::oid)) IS DISTINCT FROM
       (SELECT snapshot FROM public.artifact_upgrade_expected WHERE kind='oid') THEN
  RAISE EXCEPTION 'artifact upgrade changed aggregate identity or merged names';
 END IF;
 IF EXISTS(SELECT 1 FROM public.artifact_upgrade_expected expected
   LEFT JOIN control_plane.artifact_history history ON history.ref=expected.ref
   WHERE expected.kind='artifact' AND expected.snapshot IS DISTINCT FROM to_jsonb(history)-ARRAY['revision_id','revision_ref']) OR
    EXISTS(SELECT 1 FROM public.artifact_upgrade_expected expected LEFT JOIN control_plane.artifacts artifact ON artifact.ref=expected.ref
      LEFT JOIN control_plane.artifact_content content ON content.artifact_id=artifact.id
      WHERE expected.kind='content' AND expected.snapshot IS DISTINCT FROM to_jsonb(content)) OR
    EXISTS(SELECT 1 FROM public.artifact_upgrade_expected expected LEFT JOIN control_plane.artifact_heads head ON head.ref=expected.ref
      LEFT JOIN control_plane.artifact_bindings binding ON binding.artifact_id=head.id
      WHERE expected.kind='binding' AND expected.snapshot IS DISTINCT FROM to_jsonb(binding)-ARRAY['revision_id','artifact_revision_ref','artifact_revision']) OR
    EXISTS(SELECT 1 FROM public.artifact_upgrade_expected expected LEFT JOIN control_plane.artifact_download_grants grant_row ON grant_row.ref=expected.ref
      WHERE expected.kind='grant' AND expected.snapshot IS DISTINCT FROM to_jsonb(grant_row)-ARRAY['revision_id','artifact_revision_ref','artifact_revision']) THEN
  RAISE EXCEPTION 'artifact upgrade rewrote metadata, receipt or semantic pin';
 END IF;
END $$;
COMMIT;
