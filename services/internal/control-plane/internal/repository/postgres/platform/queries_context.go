package platform

import _ "embed"

var (
	//go:embed sql/memory_revision_get.sql
	queryMemoryRevisionGet string
	//go:embed sql/memory_record_list.sql
	queryMemoryRecordList string
	//go:embed sql/memory_revision_list.sql
	queryMemoryRevisionList string
	//go:embed sql/memory_record_get.sql
	queryMemoryRecordGet string
	//go:embed sql/memory_record_lock.sql
	queryMemoryRecordLock string
	//go:embed sql/memory_record_insert.sql
	queryMemoryRecordInsert string
	//go:embed sql/memory_revision_insert.sql
	queryMemoryRevisionInsert string
	//go:embed sql/memory_record_set_revision.sql
	queryMemoryRecordSetRevision string
	//go:embed sql/memory_record_set_state.sql
	queryMemoryRecordSetState string
	//go:embed sql/memory_record_purge.sql
	queryMemoryRecordPurge string
	//go:embed sql/memory_record_disable_bindings.sql
	queryMemoryRecordDisableBindings string
)
