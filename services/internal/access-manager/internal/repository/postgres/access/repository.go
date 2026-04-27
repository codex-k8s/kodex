package access

import "embed"

// SQLFiles contains named SQL queries for the access-manager PostgreSQL repository.
//
//go:embed sql/*.sql
var SQLFiles embed.FS
