-- name: platform__artifacts_uploadartifact_5 :exec
INSERT INTO control_plane.audit_events(ref,organization_id,project_id,actor_id,action,resource_kind,resource_ref,outcome,safe_summary,correlation_ref) VALUES($1,$2::uuid,$3::uuid,$4::uuid,'artifact.upload','ARTIFACT',$5,'SUCCEEDED','Файл загружен',$6)
