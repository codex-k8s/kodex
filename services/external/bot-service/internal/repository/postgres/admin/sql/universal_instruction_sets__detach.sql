-- name: universal_instruction_sets__detach :one
with target as materialized (
	select
		agent.id as agent_id,
		role_definition.id as role_definition_id,
		instruction_set.id as instruction_set_id
	from matter_codex_agents agent
	join matter_codex_role_definitions role_definition
		on role_definition.id = agent.role_definition_id
		and role_definition.organization_scope = agent.organization_scope
	join matter_codex_instruction_sets instruction_set
		on instruction_set.id = agent.instruction_set_id
		and instruction_set.organization_scope = agent.organization_scope
	where agent.legacy_agent_role_id = $1
		and (
			agent.managed_by = 'git'
			or role_definition.managed_by = 'git'
			or instruction_set.managed_by = 'git'
		)
	for update of agent, role_definition, instruction_set
),
updated_instruction_set as (
	update matter_codex_instruction_sets instruction_set
	set managed_by = 'ui',
		source_type = 'ui_markdown',
		provenance = (
			case
				when jsonb_typeof(instruction_set.provenance) = 'object' then instruction_set.provenance
				else jsonb_build_object('legacy_provenance', instruction_set.provenance)
			end
		) || jsonb_build_object(
			'detach_history',
			case
				when jsonb_typeof(instruction_set.provenance -> 'detach_history') = 'array'
					then instruction_set.provenance -> 'detach_history'
				else '[]'::jsonb
			end || jsonb_build_array(jsonb_build_object(
				'event_version', 1,
				'detached_from', instruction_set.managed_by,
				'source_ref', instruction_set.source_ref,
				'source_revision', instruction_set.source_revision,
				'previous_provenance', instruction_set.provenance,
				'actor_ref', $2::text,
				'detached_at', now()
			))
		),
		record_version = instruction_set.record_version + 1,
		updated_at = now()
	from target
	where instruction_set.id = target.instruction_set_id
		and instruction_set.managed_by = 'git'
	returning instruction_set.id
),
updated_agent as (
	update matter_codex_agents agent
	set managed_by = 'ui',
		provenance = (
			case
				when jsonb_typeof(agent.provenance) = 'object' then agent.provenance
				else jsonb_build_object('legacy_provenance', agent.provenance)
			end
		) || jsonb_build_object(
			'detach_history',
			case
				when jsonb_typeof(agent.provenance -> 'detach_history') = 'array'
					then agent.provenance -> 'detach_history'
				else '[]'::jsonb
			end || jsonb_build_array(jsonb_build_object(
				'event_version', 1,
				'detached_from', agent.managed_by,
				'source_ref', agent.source_ref,
				'source_revision', agent.source_revision,
				'previous_provenance', agent.provenance,
				'actor_ref', $2::text,
				'detached_at', now()
			))
		),
		record_version = agent.record_version + 1,
		updated_at = now()
	from target
	where agent.id = target.agent_id
		and agent.managed_by = 'git'
	returning agent.id
),
updated_role_definition as (
	update matter_codex_role_definitions role_definition
	set managed_by = 'ui',
		provenance = (
			case
				when jsonb_typeof(role_definition.provenance) = 'object' then role_definition.provenance
				else jsonb_build_object('legacy_provenance', role_definition.provenance)
			end
		) || jsonb_build_object(
			'detach_history',
			case
				when jsonb_typeof(role_definition.provenance -> 'detach_history') = 'array'
					then role_definition.provenance -> 'detach_history'
				else '[]'::jsonb
			end || jsonb_build_array(jsonb_build_object(
				'event_version', 1,
				'detached_from', role_definition.managed_by,
				'source_ref', role_definition.source_ref,
				'source_revision', role_definition.source_revision,
				'previous_provenance', role_definition.provenance,
				'actor_ref', $2::text,
				'detached_at', now()
			))
		),
		record_version = role_definition.record_version + 1,
		updated_at = now()
	from target
	where role_definition.id = target.role_definition_id
		and role_definition.managed_by = 'git'
	returning role_definition.id
)
select count(*) from target;
