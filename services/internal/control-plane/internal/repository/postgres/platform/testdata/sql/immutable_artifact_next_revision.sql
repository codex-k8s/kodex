INSERT INTO control_plane.artifact_revisions
    (ref,artifact_id,revision,file_name,media_type,size_bytes,digest,source,scan_state,object_receipt_ref,preview_state,created_by,created_at)
SELECT 'arv_'||replace(gen_random_uuid()::text,'-',''),head.id,old.revision+1,
       'new-body.txt',old.media_type,$2,$3,old.source,'CLEAN',old.object_receipt_ref,'AVAILABLE',head.created_by,clock_timestamp()
FROM control_plane.artifact_heads head
JOIN control_plane.artifact_revisions old ON old.id=head.current_revision_id
WHERE head.ref=$1 RETURNING id::text;
