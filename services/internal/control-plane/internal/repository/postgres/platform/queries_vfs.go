package platform

import _ "embed"

// queryVFSListNodes embeds virtual project/resource/context nodes, canonical target/source-run
// eligibility before pagination, exact historical input/skill/avatar content pins and visible
// lifecycle filtering. TREE/SEARCH share the same authorized projection; total/cursor ordering
// operate after eligibility, and returned management/action hints do not execute commands.
// queryVFSListNodes встраивает virtual project/resource/context nodes, canonical target/source-run
// eligibility до pagination, exact historical input/skill/avatar content pins и visible lifecycle
// filtering. TREE/SEARCH используют одну authorized projection; total/cursor ordering применяются
// после eligibility, returned management/action hints не выполняют commands.
//
//go:embed sql/vfs_list_nodes.sql
var queryVFSListNodes string
