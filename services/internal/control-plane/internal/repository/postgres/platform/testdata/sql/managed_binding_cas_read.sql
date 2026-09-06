SELECT revision.ref, binding.version
FROM control_plane.managed_configuration_bindings binding
JOIN control_plane.managed_configuration_revisions revision ON revision.id = binding.configuration_revision_id
WHERE binding.organization_id = $1::uuid AND binding.configuration_kind = 'SYSTEM_STT'
  AND binding.consumer_kind = 'STT_SERVICE' AND binding.consumer_ref = 'stt-tts-service';
