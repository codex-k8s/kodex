-- name: assistant_configuration_component_profile_clone :exec
INSERT INTO control_plane.runtime_profiles(stable_key,name,provider,model,runtime_revision,resource_limits,enabled,version)
SELECT @target_ref,'Synthetic alternative assistant profile',provider,model,runtime_revision || '-fixture-alternative',resource_limits,true,1
FROM control_plane.runtime_profiles WHERE stable_key=@source_ref;
