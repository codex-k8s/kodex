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

func validateQueries() error {
	for name, query := range map[string]string{
		"lifecycle__acquire_lease":      acquireLeaseSQL,
		"lifecycle__validate_lease":     validateLeaseSQL,
		"lifecycle__reconcile_identity": reconcileIdentitySQL,
		"lifecycle__retire_identity":    retireIdentitySQL,
		"lifecycle__read_generations":   readGenerationsSQL,
	} {
		if strings.TrimSpace(query) == "" ||
			!strings.HasPrefix(strings.TrimSpace(query), "-- name: "+name+" ") {
			return errors.New("invalid embedded credential lifecycle query")
		}
	}
	return nil
}
