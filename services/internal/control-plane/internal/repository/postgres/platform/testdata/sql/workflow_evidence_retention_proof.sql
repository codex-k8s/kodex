-- name: workflow_evidence_retention_proof :one
-- Только наблюдение owner metadata в изолированной native schema.
WITH report AS (SELECT * FROM control_plane.artifact_history WHERE ref=$1 AND revision=1),
log AS (SELECT * FROM control_plane.artifact_history WHERE ref=$2 AND revision=1),
foreign_file AS (SELECT * FROM control_plane.artifact_history WHERE ref=$3 AND revision=1)
SELECT control_plane.workflow_evidence_retains_revision(report.organization_id,report.id,report.revision_id),
       control_plane.workflow_evidence_retains_revision(log.organization_id,log.id,log.revision_id),
       control_plane.workflow_evidence_retains_revision(foreign_file.organization_id,foreign_file.id,foreign_file.revision_id),
       control_plane.workflow_evidence_retains_revision(report.organization_id,report.id,foreign_file.revision_id),
       control_plane.artifact_has_retained_revisions(report.id),control_plane.artifact_has_retained_revisions(log.id)
FROM report CROSS JOIN log CROSS JOIN foreign_file;
