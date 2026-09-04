package platform

import _ "embed"

//go:embed sql/vfs_list_nodes.sql
var queryVFSListNodes string
