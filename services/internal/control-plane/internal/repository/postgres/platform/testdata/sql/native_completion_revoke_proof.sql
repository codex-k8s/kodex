SELECT jsonb_build_object(
 'WriterOpen', storage.native_writer_lease_ref IS NOT NULL,
 'Checkpoint', storage.native_checkpoint IS NOT NULL,
 'Leases', (SELECT count(*) FROM control_plane.runtime_leases lease WHERE lease.run_id=run.id AND lease.state='CLAIMED'),
 'Artifacts', (SELECT count(*) FROM control_plane.artifact_heads artifact WHERE artifact.run_id=run.id),
 'ArtifactEvents', (SELECT count(*) FROM control_plane.run_events event WHERE event.root_run_id=run.root_run_id AND event.type='ARTIFACT_AVAILABLE'),
 'Events', (SELECT count(*) FROM control_plane.run_events event WHERE event.root_run_id=run.root_run_id)
) FROM control_plane.runs run
JOIN control_plane.session_storage storage ON storage.session_id=run.session_id AND storage.organization_id=run.organization_id
WHERE run.ref=$1;
