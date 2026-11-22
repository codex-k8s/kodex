-- name: ssh_execution__receipt_source :one
WITH stamp AS MATERIALIZED(SELECT clock_timestamp() AS at)
SELECT invocation.operation,invocation.bounded_input,invocation.input_digest,original_root.ref
FROM control_plane.integration_invocations invocation
JOIN control_plane.ssh_execution_intents intent ON intent.invocation_id=invocation.id AND intent.organization_id=invocation.organization_id
JOIN control_plane.ssh_invocation_identity_bindings pin ON pin.invocation_id=invocation.id AND pin.organization_id=invocation.organization_id
JOIN control_plane.runs original_run ON original_run.id=invocation.run_id AND original_run.organization_id=invocation.organization_id
JOIN control_plane.runs original_root ON original_root.id=original_run.root_run_id AND original_root.organization_id=original_run.organization_id
 AND original_root.root_run_id=original_root.id
JOIN control_plane.integration_invocations reader ON reader.id=@reader_invocation_id::uuid AND reader.organization_id=invocation.organization_id
 AND reader.connection_id=invocation.connection_id
JOIN control_plane.ssh_invocation_identity_bindings reader_pin ON reader_pin.invocation_id=reader.id AND reader_pin.organization_id=reader.organization_id
 AND reader_pin.connection_id=reader.connection_id AND reader_pin.identity_revision_id=pin.identity_revision_id
 AND reader_pin.subject_digest=pin.subject_digest AND reader_pin.transport_mode='MANAGED'
JOIN control_plane.runs reader_run ON reader_run.id=reader.run_id AND reader_run.organization_id=reader.organization_id
 AND reader_run.project_id IS NOT DISTINCT FROM original_run.project_id
JOIN control_plane.runs reader_root ON reader_root.id=reader_run.root_run_id AND reader_root.organization_id=reader_run.organization_id
 AND reader_root.root_run_id=reader_root.id AND reader_root.initiated_by=original_root.initiated_by
 AND reader_root.project_id IS NOT DISTINCT FROM original_root.project_id
JOIN control_plane.subjects actor ON actor.id=reader_root.initiated_by AND actor.organization_id=reader.organization_id AND actor.active
JOIN control_plane.catalog_access_targets original_target ON original_target.organization_id=original_root.organization_id
 AND original_target.kind='RUN' AND original_target.id=original_root.id
CROSS JOIN stamp
WHERE invocation.organization_id=@organization_id::uuid AND invocation.ref=@invocation_ref
 AND intent.input_digest=invocation.input_digest
 AND intent.identity_revision_id=@identity_id::uuid AND pin.identity_revision_id=intent.identity_revision_id
 AND pin.connection_id=invocation.connection_id AND pin.root_run_id=original_root.id AND pin.root_actor_id=original_root.initiated_by
 AND reader_pin.root_run_id=reader_root.id AND reader_pin.root_actor_id=actor.id
 AND pin.subject_digest=@subject_digest AND pin.transport_mode='MANAGED'
 AND control_plane.catalog_resource_visible(invocation.organization_id,actor.id,'run.view','RUN',original_target.id,
  original_target.project_id,original_target.owner_id,original_target.related_ids,stamp.at,false);
