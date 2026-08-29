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
)
