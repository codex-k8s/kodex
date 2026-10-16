-- Только disposable PostgreSQL: отрицательная попытка записи без полного proof.
INSERT INTO control_plane.artifact_content(artifact_id,object_key,object_version,object_etag,digest,size_bytes,antivirus_report)
SELECT id,'disposable/unproven/content','unproven-version','unproven-etag',digest,size_bytes,$2
FROM control_plane.artifacts WHERE ref=$1;
