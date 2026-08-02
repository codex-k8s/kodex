package postgresinbox

import (
	"context"
	_ "embed"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

//go:embed schema.sql
var testSchemaContract string

func TestStrictNamedArgumentsMatchEveryProductionQuery(t *testing.T) {
	t.Parallel()
	queries, err := loadQueries()
	if err != nil {
		t.Fatalf("loadQueries() error = %v", err)
	}
	tests := []struct {
		name  string
		query string
		args  pgx.StrictNamedArgs
	}{
		{"schema path", queries.schemaSetSearchPath, named("schema_name")},
		{"cursor ensure", queries.cursorEnsure, named("consumer_name", "consumer_scope", "ordering_key")},
		{"cursor get", queries.cursorGetForUpdate, named("consumer_name", "consumer_scope", "ordering_key")},
		{"cursor fence", queries.cursorTakeFence, named("consumer_name", "consumer_scope", "ordering_key")},
		{"cursor advance", queries.cursorAdvance, named("consumer_name", "consumer_scope", "ordering_key", "event_sequence", "event_id", "event_digest")},
		{"inbox get", queries.inboxGetByEventForUpdate, named("consumer_name", "consumer_scope", "event_id")},
		{"inbox read", queries.inboxGetByEvent, named("consumer_name", "consumer_scope", "event_id")},
		{"inbox sequence", queries.inboxGetBySequenceForUpdate, named("consumer_name", "consumer_scope", "ordering_key", "event_sequence")},
		{"inbox receive", queries.inboxInsertReceived, withNames(eventNamed(false), "error_code")},
		{"inbox stale", queries.inboxInsertStale, eventNamed(true)},
		{"inbox claim", queries.inboxClaim, named("consumer_name", "consumer_scope", "event_id", "lease_owner", "lease_token", "lease_fence", "lease_seconds")},
		{"inbox complete", queries.inboxComplete, named("consumer_name", "consumer_scope", "event_id", "event_digest", "lease_owner", "lease_token", "lease_generation", "lease_fence", "retention_seconds")},
		{"inbox retry", queries.inboxMarkRetry, named("consumer_name", "consumer_scope", "event_id", "event_digest", "lease_owner", "lease_token", "lease_generation", "lease_fence", "backoff_seconds", "error_code")},
		{"inbox dead letter", queries.inboxMarkDeadLetter, named("consumer_name", "consumer_scope", "event_id", "event_digest", "lease_owner", "lease_token", "lease_generation", "lease_fence", "error_code")},
		{"inbox exhaustion", queries.inboxExpireToDeadLetter, named("consumer_name", "consumer_scope", "event_id", "event_digest", "error_code")},
		{"inbox renew", queries.inboxRenew, named("consumer_name", "consumer_scope", "event_id", "event_digest", "lease_owner", "lease_token", "lease_generation", "lease_fence", "lease_seconds")},
		{"inbox cleanup", queries.inboxCleanup, named("retention_seconds", "batch_size")},
		{"repair read", queries.repairGetByIdempotency, named("consumer_name", "consumer_scope", "idempotency_key")},
		{"repair insert", queries.repairInsert, named("consumer_name", "consumer_scope", "idempotency_key", "request_digest", "repair_id", "event_id", "event_digest", "expected_generation", "expected_fence", "actor", "reason", "evidence_digest", "result_generation", "result_fence")},
		{"inbox requeue", queries.inboxRequeue, named("consumer_name", "consumer_scope", "event_id", "event_digest", "event_sequence", "expected_generation", "expected_fence")},
		{"schema inspect", queries.schemaInspect, named("schema_name", "schema_component")},
		{"schema probe", queries.schemaProbe, named()},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, _, rewriteErr := test.args.RewriteQuery(
				context.Background(),
				nil,
				test.query,
				nil,
			)
			if rewriteErr != nil {
				t.Fatalf("RewriteQuery() error = %v", rewriteErr)
			}
		})
	}
}

func TestSchemaMarkerIsInstalledAfterCompleteContract(t *testing.T) {
	t.Parallel()
	marker := strings.LastIndex(testSchemaContract, "INSERT INTO runtime_event_schema_versions")
	lastIndex := strings.LastIndex(testSchemaContract, "CREATE INDEX runtime_inbox_repairs_event_idx")
	if marker < 0 || lastIndex < 0 || marker < lastIndex {
		t.Fatal("schema marker is not installed after required objects")
	}
	if !strings.Contains(testSchemaContract, schemaDigestHex) {
		t.Fatal("schema marker digest differs from runtime contract")
	}
}

func TestReadinessManifestCoversNamedConstraintsAndIndexes(t *testing.T) {
	t.Parallel()
	required := requiredSchemaObjects()
	constraintPattern := regexp.MustCompile(`CONSTRAINT\s+([a-z0-9_]+)`)
	for _, match := range constraintPattern.FindAllStringSubmatch(testSchemaContract, -1) {
		if !manifestHasObjectSuffix(required, "constraint/", "."+match[1]) {
			t.Fatalf("constraint %s is absent from readiness manifest", match[1])
		}
	}
	indexPattern := regexp.MustCompile(`CREATE INDEX\s+([a-z0-9_]+)`)
	for _, match := range indexPattern.FindAllStringSubmatch(testSchemaContract, -1) {
		if _, ok := required["index/"+match[1]]; !ok {
			t.Fatalf("index %s is absent from readiness manifest", match[1])
		}
	}
}

func TestReadinessQueriesKeepSecurityBoundaryClosed(t *testing.T) {
	t.Parallel()
	pathFragments := []string{
		"pg_catalog.set_config",
		"pg_catalog,%I,pg_temp",
		"current_user = session_user",
	}
	for _, fragment := range pathFragments {
		if !strings.Contains(rawSchemaSetSearchPath, fragment) {
			t.Fatalf("search path query misses %s", fragment)
		}
	}
	inspectFragments := []string{
		"pg_catalog.pg_has_role",
		"relation.relrowsecurity",
		"relation.relhastriggers",
		"pg_catalog.pg_inherits",
		"index_record.indisvalid",
		"'CREATE'",
		"'TRUNCATE'",
		"'MAINTAIN'",
	}
	for _, fragment := range inspectFragments {
		if !strings.Contains(rawSchemaInspect, fragment) {
			t.Fatalf("schema inspection misses %s", fragment)
		}
	}
}

func manifestHasObjectSuffix(
	manifest map[string]string,
	prefix string,
	suffix string,
) bool {
	for key := range manifest {
		if strings.HasPrefix(key, prefix) && strings.HasSuffix(key, suffix) {
			return true
		}
	}
	return false
}

func named(names ...string) pgx.StrictNamedArgs {
	arguments := make(pgx.StrictNamedArgs, len(names))
	for _, name := range names {
		arguments[name] = nil
	}
	return arguments
}

func eventNamed(includeRetention bool) pgx.StrictNamedArgs {
	arguments := named(
		"consumer_name",
		"consumer_scope",
		"event_id",
		"event_digest",
		"event_name",
		"event_version",
		"schema_version",
		"occurred_at",
		"organization_id",
		"aggregate_type",
		"aggregate_id",
		"aggregate_version",
		"event_sequence",
		"max_attempts",
		"max_repairs",
	)
	if includeRetention {
		arguments["retention_seconds"] = nil
	}
	return arguments
}

func withNames(arguments pgx.StrictNamedArgs, names ...string) pgx.StrictNamedArgs {
	for _, name := range names {
		arguments[name] = nil
	}
	return arguments
}
