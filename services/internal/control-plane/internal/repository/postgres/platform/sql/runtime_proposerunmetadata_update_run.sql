-- name: runtime_proposerunmetadata_update_run :exec
UPDATE control_plane.runs
SET title=CASE WHEN title_source='USER_EDITED' OR $2='' THEN title ELSE $2 END,
    title_source=CASE WHEN title_source='USER_EDITED' OR $2='' THEN title_source ELSE 'AGENT_PROPOSED' END,
    presentation_metadata=jsonb_build_object('activitySummary',$3),version=version+1,updated_at=clock_timestamp()
WHERE id=$1::uuid
