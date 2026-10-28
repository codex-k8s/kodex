-- name: workflow_artifact_boundary_counts :one
SELECT (SELECT count(*) FROM control_plane.runs run WHERE run.project_id=project.id),
       (SELECT count(*) FROM control_plane.artifact_heads head WHERE head.project_id=project.id),
       (SELECT count(*) FROM control_plane.agents agent WHERE agent.project_id=project.id)
FROM control_plane.projects project WHERE project.ref=@project_ref;
