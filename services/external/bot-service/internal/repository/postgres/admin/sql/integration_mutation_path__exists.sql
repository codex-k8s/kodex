-- name: integration_mutation_path__exists :one
select exists(
	select 1
	from matter_codex_agent_sessions session
	join matter_codex_agent_roles role on role.id = session.role_id
	where session.role_id = $1
		and session.session_key = $2
		and (
			lower(btrim(role.kubernetes_access)) = 'cluster-admin'
			or lower(btrim(coalesce(session.capabilities ->> 'kubernetes_access', ''))) = 'cluster-admin'
			or exists(
				select 1
				from jsonb_array_elements(
					case
						when jsonb_typeof(session.capabilities -> 'runtime_env') = 'array'
							then session.capabilities -> 'runtime_env'
						else '[]'::jsonb
					end
				) runtime_env
				where matter_codex_integration_direct_kubernetes_env(runtime_env ->> 'name')
			)
			or exists(
				select 1
				from matter_codex_agent_role_runtime_variables binding
				join matter_codex_project_runtime_variables variable on variable.id = binding.variable_id
				where binding.role_id = session.role_id
					and variable.enabled
					and matter_codex_integration_direct_kubernetes_env(variable.name)
			)
		)
);
