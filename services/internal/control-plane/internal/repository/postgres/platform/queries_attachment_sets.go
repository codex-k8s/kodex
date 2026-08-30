package platform

import _ "embed"

var (
	//go:embed sql/attachmentsets_select_artifacts.sql
	queryAttachmentSetsSelectArtifacts string
	//go:embed sql/attachmentsets_insert_set.sql
	queryAttachmentSetsInsertSet string
	//go:embed sql/attachmentsets_insert_item.sql
	queryAttachmentSetsInsertItem string
	//go:embed sql/attachmentsets_insert_binding.sql
	queryAttachmentSetsInsertBinding string
	//go:embed sql/attachmentsets_bind_run.sql
	queryAttachmentSetsBindRun string
	//go:embed sql/attachmentsets_bind_turn.sql
	queryAttachmentSetsBindTurn string
	//go:embed sql/attachmentsets_bind_gate_resolution.sql
	queryAttachmentSetsBindGateResolution string
	//go:embed sql/attachmentsets_select_timestamps.sql
	queryAttachmentSetsSelectTimestamps string
	//go:embed sql/attachmentsets_select_family.sql
	queryAttachmentSetsSelectFamily string
	//go:embed sql/attachmentsets_lock_family.sql
	queryAttachmentSetsLockFamily string
	//go:embed sql/attachmentsets_select_latest.sql
	queryAttachmentSetsSelectLatest string
	//go:embed sql/attachmentsets_resolve_finalized.sql
	queryAttachmentSetsResolveFinalized string
	//go:embed sql/attachmentsets_list_items.sql
	queryAttachmentSetsListItems string
	//go:embed sql/attachmentsets_get.sql
	queryAttachmentSetsGet string
	//go:embed sql/attachmentsets_get_items.sql
	queryAttachmentSetsGetItems string
	//go:embed sql/attachmentsets_project_by_ref.sql
	queryAttachmentSetsProjectByRef string
)
