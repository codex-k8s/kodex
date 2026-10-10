INSERT INTO control_plane.artifact_revision_content (revision_id,object_key,object_version,object_etag,digest,size_bytes)
SELECT id,$2,$3,'fixture',digest,size_bytes FROM control_plane.artifact_revisions WHERE id=$1::uuid;
