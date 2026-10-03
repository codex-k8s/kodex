SELECT control_plane.purge_project_database(project.organization_id,project.id,$2)
FROM control_plane.projects project WHERE project.ref=$1;
