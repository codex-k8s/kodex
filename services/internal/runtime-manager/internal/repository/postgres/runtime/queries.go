package runtime

import "fmt"

var (
	queryCommandResultGet                 = mustLoadQuery("command_result__get")
	queryCommandResultInsert              = mustLoadQuery("command_result__insert")
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
