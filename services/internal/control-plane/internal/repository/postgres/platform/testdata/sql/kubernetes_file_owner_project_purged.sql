-- name: kubernetes_file_owner_project_purged :one
SELECT
 (SELECT count(*) FROM control_plane.kubernetes_file_input_sources WHERE organization_id=$1::uuid AND id=$2::uuid),
 (SELECT count(*) FROM control_plane.kubernetes_file_input_frames WHERE organization_id=$1::uuid AND source_id=$2::uuid),
 -- Runtime не читает частные admission rows. После подтверждённого commit удаления
 -- source точный enforced NOT NULL FK доказывает отсутствие admission без родителя.
 (SELECT count(*) FROM pg_constraint bound
  JOIN pg_attribute source_key ON source_key.attrelid=bound.conrelid AND source_key.attname='source_id' AND source_key.attnotnull
  JOIN pg_attribute tenant_key ON tenant_key.attrelid=bound.conrelid AND tenant_key.attname='organization_id' AND tenant_key.attnotnull
  JOIN pg_attribute source_id ON source_id.attrelid=bound.confrelid AND source_id.attname='id' AND source_id.attnotnull
  JOIN pg_attribute source_tenant ON source_tenant.attrelid=bound.confrelid AND source_tenant.attname='organization_id' AND source_tenant.attnotnull
  WHERE bound.conrelid='control_plane.kubernetes_file_delete_admissions'::regclass
   AND bound.confrelid='control_plane.kubernetes_file_input_sources'::regclass
   AND bound.contype='f' AND bound.convalidated AND bound.conenforced
   AND bound.confdeltype='a' AND bound.confupdtype='a' AND bound.confmatchtype='s'
   AND bound.condeferrable AND NOT bound.condeferred
   AND bound.conkey=ARRAY[source_key.attnum,tenant_key.attnum]
   AND bound.confkey=ARRAY[source_id.attnum,source_tenant.attnum]),
 (SELECT count(*) FROM control_plane.kubernetes_file_input_origins WHERE organization_id=$1::uuid AND invocation_id=$3::uuid),
 (SELECT count(*) FROM control_plane.integration_invocations WHERE organization_id=$1::uuid AND id=$3::uuid),
 (SELECT count(*) FROM control_plane.kubernetes_execution_streams WHERE organization_id=$1::uuid AND id=$5::uuid),
 (SELECT count(*) FROM control_plane.kubernetes_input_stages WHERE organization_id=$1::uuid AND stream_id=$5::uuid),
 (SELECT count(*) FROM control_plane.prepared_content WHERE organization_id=$1::uuid AND id=$4::uuid
  AND source_profile='KUBERNETES_FILE_SOURCE' AND source_profile_ref=$6 AND state='CLEANED'
  AND project_id IS NULL AND adopted_file_source_id IS NULL AND deletion_receipt_digest IS NOT NULL
  AND object_key IS NULL AND object_version IS NULL AND object_etag IS NULL);
