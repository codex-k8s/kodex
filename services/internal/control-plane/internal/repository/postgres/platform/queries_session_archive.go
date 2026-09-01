package platform

import _ "embed"

var (
	//go:embed sql/session_archive_materialize_tasks.sql
	querySessionArchiveMaterializeTasks string
	//go:embed sql/session_archive_select_claimable_tasks.sql
	querySessionArchiveSelectClaimableTasks string
	//go:embed sql/session_archive_claim_task.sql
	querySessionArchiveClaimTask string
	//go:embed sql/session_archive_renew_task.sql
	querySessionArchiveRenewTask string
	//go:embed sql/session_archive_lock_task.sql
	querySessionArchiveLockTask string
	//go:embed sql/session_archive_complete_snapshot.sql
	querySessionArchiveCompleteSnapshot string
	//go:embed sql/session_archive_complete_restore.sql
	querySessionArchiveCompleteRestore string
	//go:embed sql/session_archive_complete_pvc_deletion.sql
	querySessionArchiveCompletePVCDeletion string
	//go:embed sql/session_archive_complete_object_deletion.sql
	querySessionArchiveCompleteObjectDeletion string
	//go:embed sql/session_archive_fail_task.sql
	querySessionArchiveFailTask string
)
