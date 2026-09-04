-- name: context_binding_memory_target :one
SELECT record.id::text,record.project_id::text,COALESCE(record.agent_id::text,''),revision.id::text,revision.ref,revision.digest,
    record.state='ACTIVE' AND revision.retention_until>statement_timestamp() AND current.retention_until>statement_timestamp()
FROM control_plane.memory_records record
JOIN control_plane.memory_record_revisions revision ON revision.record_id=record.id AND revision.ref=$3
JOIN control_plane.memory_record_revisions current ON current.id=record.current_revision_id
WHERE record.organization_id=$1::uuid AND record.ref=$2 FOR SHARE OF record,revision;
