package credentiallifecycle

import (
	_ "embed"
	"errors"
	"strings"
)

//go:embed sql/lifecycle__acquire_lease.sql
var acquireLeaseSQL string

//go:embed sql/lifecycle__validate_lease.sql
var validateLeaseSQL string

//go:embed sql/lifecycle__reconcile_identity.sql
var reconcileIdentitySQL string

//go:embed sql/lifecycle__retire_identity.sql
var retireIdentitySQL string

//go:embed sql/lifecycle__read_generations.sql
var readGenerationsSQL string

//go:embed sql/lifecycle__load_or_create_intent.sql
var loadOrCreateIntentSQL string

//go:embed sql/lifecycle__advance_intent.sql
var advanceIntentSQL string

//go:embed sql/lifecycle__read_intent.sql
var readIntentSQL string

//go:embed sql/lifecycle__read_session_readbacks.sql
var readSessionReadbacksSQL string

func validateQueries() error {
	for name, query := range map[string]string{
		"lifecycle__acquire_lease":          acquireLeaseSQL,
		"lifecycle__validate_lease":         validateLeaseSQL,
		"lifecycle__reconcile_identity":     reconcileIdentitySQL,
		"lifecycle__retire_identity":        retireIdentitySQL,
		"lifecycle__read_generations":       readGenerationsSQL,
		"lifecycle__load_or_create_intent":  loadOrCreateIntentSQL,
		"lifecycle__advance_intent":         advanceIntentSQL,
		"lifecycle__read_intent":            readIntentSQL,
		"lifecycle__read_session_readbacks": readSessionReadbacksSQL,
	} {
		if strings.TrimSpace(query) == "" ||
			!strings.HasPrefix(strings.TrimSpace(query), "-- name: "+name+" ") {
			return errors.New("invalid embedded credential lifecycle query")
		}
	}
	return nil
}
