package agent

import "fmt"

var (
	queryCommandResultCreate               = mustLoadQuery("command_result__create")
	queryCommandResultGet                  = mustLoadQuery("command_result__get")
	queryAcceptanceResultCreate            = mustLoadQuery("acceptance_result__create")
	queryAcceptanceResultGet               = mustLoadQuery("acceptance_result__get")
	queryAcceptanceResultList              = mustLoadQuery("acceptance_result__list")
	queryAcceptanceResultUpdate            = mustLoadQuery("acceptance_result__update")
	queryAgentActivityCreate               = mustLoadQuery("agent_activity__create")
	queryAgentActivityGet                  = mustLoadQuery("agent_activity__get")
	queryAgentActivityList                 = mustLoadQuery("agent_activity__list")
	queryFollowUpIntentCreate              = mustLoadQuery("follow_up_intent__create")
	queryFollowUpIntentGet                 = mustLoadQuery("follow_up_intent__get")
	queryFollowUpIntentReserveDispatch     = mustLoadQuery("follow_up_intent__reserve_dispatch")
	queryFollowUpIntentUpdate              = mustLoadQuery("follow_up_intent__update")
	queryHumanGateRequestCreate            = mustLoadQuery("human_gate_request__create")
	queryHumanGateRequestGet               = mustLoadQuery("human_gate_request__get")
	queryHumanGateRequestList              = mustLoadQuery("human_gate_request__list")
	queryHumanGateRequestUpdate            = mustLoadQuery("human_gate_request__update")
	querySelfDeployPlanCreate              = mustLoadQuery("self_deploy_plan__create")
	querySelfDeployPlanGet                 = mustLoadQuery("self_deploy_plan__get")
	querySelfDeployPlanList                = mustLoadQuery("self_deploy_plan__list")
	queryFlowCreate                        = mustLoadQuery("flow__create")
	queryFlowGet                           = mustLoadQuery("flow__get")
	queryFlowList                          = mustLoadQuery("flow__list")
	queryFlowUpdate                        = mustLoadQuery("flow__update")
	queryFlowVersionActivate               = mustLoadQuery("flow_version__activate")
	queryFlowVersionCreate                 = mustLoadQuery("flow_version__create")
	queryFlowVersionGet                    = mustLoadQuery("flow_version__get")
	queryFlowVersionList                   = mustLoadQuery("flow_version__list")
	queryFlowVersionSupersede              = mustLoadQuery("flow_version__supersede")
	queryOutboxEventClaim                  = mustLoadQuery("outbox_event__claim")
	queryOutboxEventCreate                 = mustLoadQuery("outbox_event__create")
	queryOutboxEventMarkFailed             = mustLoadQuery("outbox_event__mark_failed")
	queryOutboxEventMarkPermanent          = mustLoadQuery("outbox_event__mark_permanently_failed")
	queryOutboxEventMarkPublished          = mustLoadQuery("outbox_event__mark_published")
	queryPromptTemplateCreate              = mustLoadQuery("prompt_template__create")
	queryPromptTemplateGet                 = mustLoadQuery("prompt_template__get")
	queryPromptTemplateList                = mustLoadQuery("prompt_template__list")
	queryPromptTemplateUpdate              = mustLoadQuery("prompt_template__update")
	queryPromptVersionActivate             = mustLoadQuery("prompt_template_version__activate")
	queryPromptVersionCreate               = mustLoadQuery("prompt_template_version__create")
	queryPromptVersionGet                  = mustLoadQuery("prompt_template_version__get")
	queryPromptVersionList                 = mustLoadQuery("prompt_template_version__list")
	queryPromptVersionSupersede            = mustLoadQuery("prompt_template_version__supersede")
	queryRoleCreate                        = mustLoadQuery("role_profile__create")
	queryRoleGet                           = mustLoadQuery("role_profile__get")
	queryRoleList                          = mustLoadQuery("role_profile__list")
	queryRoleUpdate                        = mustLoadQuery("role_profile__update")
	queryRunCreate                         = mustLoadQuery("run__create")
	queryRunGet                            = mustLoadQuery("run__get")
	queryRunList                           = mustLoadQuery("run__list")
	queryRunSummaryList                    = mustLoadQuery("run_summary__list")
	queryRunUpdate                         = mustLoadQuery("run__update")
	querySessionCreate                     = mustLoadQuery("session__create")
	querySessionFindActiveByTarget         = mustLoadQuery("session__find_active_by_provider_work_item")
	querySessionGet                        = mustLoadQuery("session__get")
	querySessionSummaryList                = mustLoadQuery("session_summary__list")
	querySessionStateSnapshotCreate        = mustLoadQuery("session_state_snapshot__create")
	querySessionStateSnapshotGet           = mustLoadQuery("session_state_snapshot__get")
	querySessionUpdate                     = mustLoadQuery("session__update")
	queryStageCreate                       = mustLoadQuery("stage__create")
	queryStageListByFlowVersion            = mustLoadQuery("stage__list_by_flow_version")
	queryStageRoleBindingCreate            = mustLoadQuery("stage_role_binding__create")
	queryStageRoleBindingListByFlowVersion = mustLoadQuery("stage_role_binding__list_by_flow_version")
	queryStageTransitionCreate             = mustLoadQuery("stage_transition__create")
	queryStageTransitionListByFlowVersion  = mustLoadQuery("stage_transition__list_by_flow_version")
)

func mustLoadQuery(name string) string {
	query, err := loadQuery(name)
	if err != nil {
		panic(err)
	}
	return query
}

func loadQuery(name string) (string, error) {
	data, err := SQLFiles.ReadFile("sql/" + name + ".sql")
	if err != nil {
		return "", fmt.Errorf("load sql query %s: %w", name, err)
	}
	return string(data), nil
}
