package eventlog

import (
	"embed"
	"fmt"
)

// SQLFiles contains named SQL queries for the event log PostgreSQL store.
//
//go:embed sql/*.sql
var SQLFiles embed.FS

var (
	queryEventLogAppend             = mustLoadQuery("event_log__append")
	queryEventLogClaim              = mustLoadQuery("event_log__claim")
	queryEventLogAdvanceCheckpoint  = mustLoadQuery("event_log__advance_checkpoint")
	queryEventLogReleaseCheckpoint  = mustLoadQuery("event_log__release_checkpoint")
	queryEventLogEnsureCheckpoint   = mustLoadQuery("event_log__ensure_checkpoint")
	queryEventLogGetStoredEventByID = mustLoadQuery("event_log__get_stored_event_by_id")
	queryEventLogGetCheckpointState = mustLoadQuery("event_log__get_checkpoint_state")
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
