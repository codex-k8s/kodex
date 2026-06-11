package runtime

import "fmt"

var (
	queryCommandResultGet                 = mustLoadQuery("command_result__get")
	queryCommandResultInsert              = mustLoadQuery("command_result__insert")
	queryBuildContextGet                  = mustLoadQuery("build_context__get")
	queryBuildContextGetByFingerprint     = mustLoadQuery("build_context__get_by_fingerprint")
	queryBuildContextInsert               = mustLoadQuery("build_context__insert")
	queryBuildContextListRunnable         = mustLoadQuery("build_context__list_runnable")
	queryBuildContextUpdate               = mustLoadQuery("build_context__update")
	queryCleanupPolicyGet                 = mustLoadQuery("cleanup_policy__get")
	queryCleanupPolicyInsert              = mustLoadQuery("cleanup_policy__insert")
	queryCleanupPolicyListActive          = mustLoadQuery("cleanup_policy__list_active")
	queryCleanupPolicyUpdate              = mustLoadQuery("cleanup_policy__update")
	queryCleanupSlotClaimBlocked          = mustLoadQuery("cleanup_slot__claim_blocked")
	queryCleanupSlotClaimCleanable        = mustLoadQuery("cleanup_slot__claim_cleanable")
	queryCleanupSlotScrubJobStepTails     = mustLoadQuery("cleanup_slot__scrub_job_step_tails")
	queryCleanupSlotScrubJobTails         = mustLoadQuery("cleanup_slot__scrub_job_tails")
	queryJobClaim                         = mustLoadQuery("job__claim")
	queryJobGet                           = mustLoadQuery("job__get")
	queryJobInsert                        = mustLoadQuery("job__insert")
	queryJobList                          = mustLoadQuery("job__list")
	queryJobStepListByJobIDs              = mustLoadQuery("job_step__list_by_job_ids")
	queryJobStepUpsert                    = mustLoadQuery("job_step__upsert")
	queryJobUpdate                        = mustLoadQuery("job__update")
	queryOutboxEventClaim                 = mustLoadQuery("outbox_event__claim")
	queryOutboxEventInsert                = mustLoadQuery("outbox_event__insert")
	queryOutboxEventMarkFailed            = mustLoadQuery("outbox_event__mark_failed")
	queryOutboxEventMarkPermanentlyFailed = mustLoadQuery("outbox_event__mark_permanently_failed")
	queryOutboxEventMarkPublished         = mustLoadQuery("outbox_event__mark_published")
	queryRuntimeArtifactRefGet            = mustLoadQuery("runtime_artifact_ref__get")
	queryRuntimeArtifactRefInsert         = mustLoadQuery("runtime_artifact_ref__insert")
	queryRuntimeArtifactRefList           = mustLoadQuery("runtime_artifact_ref__list")
	queryPrewarmPoolCountSlots            = mustLoadQuery("prewarm_pool__count_slots")
	queryPrewarmPoolGet                   = mustLoadQuery("prewarm_pool__get")
	queryPrewarmPoolGetForUpdate          = mustLoadQuery("prewarm_pool__get_for_update")
	queryPrewarmPoolInsert                = mustLoadQuery("prewarm_pool__insert")
	queryPrewarmPoolListExcessSlots       = mustLoadQuery("prewarm_pool__list_excess_slots")
	queryPrewarmPoolUpdate                = mustLoadQuery("prewarm_pool__update")
	querySlotClaimReusable                = mustLoadQuery("slot__claim_reusable")
	querySlotGet                          = mustLoadQuery("slot__get")
	querySlotInsert                       = mustLoadQuery("slot__insert")
	querySlotList                         = mustLoadQuery("slot__list")
	querySlotUpdate                       = mustLoadQuery("slot__update")
	queryWorkspaceMaterializationGet      = mustLoadQuery("workspace_materialization__get")
	queryWorkspaceMaterializationInsert   = mustLoadQuery("workspace_materialization__insert")
	queryWorkspaceMaterializationList     = mustLoadQuery("workspace_materialization__list")
	queryWorkspaceMaterializationUpdate   = mustLoadQuery("workspace_materialization__update")
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
