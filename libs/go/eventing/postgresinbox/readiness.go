package postgresinbox

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Check сверяет точный schema contract и рабочий runtime principal path.
func (processor *Processor) Check(ctx context.Context) (err error) {
	if err := processor.enter(); err != nil {
		return err
	}
	defer processor.leave()
	ctx, span := processor.tracer.Start(ctx, OperationCheck)
	outcome := OutcomeError
	defer func() {
		if err == nil {
			outcome = OutcomeReady
		} else if isContextDone(err) {
			outcome = OutcomeCanceled
		}
		span.End(outcome)
		processor.observer.Observe(OperationCheck, outcome)
	}()

	err = processor.retryTransaction(ctx, func(tx pgx.Tx) error {
		rows, queryErr := tx.Query(
			ctx,
			processor.queries.schemaInspect,
			pgx.StrictNamedArgs{
				"schema_name":      processor.config.Schema,
				"schema_component": schemaComponent,
			},
		)
		if queryErr != nil {
			return schemaMismatch(queryErr)
		}
		actual := make(map[string]string)
		for rows.Next() {
			var kind string
			var name string
			var signature string
			if scanErr := rows.Scan(&kind, &name, &signature); scanErr != nil {
				rows.Close()
				return schemaMismatch(scanErr)
			}
			key := kind + "/" + name
			if _, duplicate := actual[key]; duplicate {
				rows.Close()
				return ErrSchemaMismatch
			}
			actual[key] = signature
		}
		rows.Close()
		if rows.Err() != nil {
			return schemaMismatch(rows.Err())
		}
		if validateSchemaObjects(actual) != nil {
			return ErrSchemaMismatch
		}
		for _, operation := range processor.effectOperations {
			var effectReady bool
			if probeErr := tx.QueryRow(
				ctx,
				processor.queries.effectInspect,
				pgx.StrictNamedArgs{
					"schema_name":   operation.schema,
					"function_name": operation.function,
				},
			).Scan(&effectReady); probeErr != nil || !effectReady {
				return schemaMismatch(probeErr)
			}
		}

		var workingPathReady bool
		probeErr := tx.QueryRow(ctx, processor.queries.schemaProbe).Scan(&workingPathReady)
		if probeErr != nil || !workingPathReady {
			return schemaMismatch(probeErr)
		}
		return nil
	})
	return err
}

func validateSchemaObjects(actual map[string]string) error {
	for key, expected := range requiredSchemaObjects() {
		if actual[key] != expected {
			return ErrSchemaMismatch
		}
		delete(actual, key)
	}
	for key, signature := range actual {
		if !strings.HasPrefix(key, "extension_index/") || signature != "1" {
			return ErrSchemaMismatch
		}
	}
	return nil
}

func schemaMismatch(cause error) error {
	if cause == nil {
		return ErrSchemaMismatch
	}
	return wrapSafe(errorTextSchemaMismatch, errors.Join(ErrSchemaMismatch, cause))
}

func requiredSchemaObjects() map[string]string {
	required := map[string]string{
		"marker/postgresinbox": strconv.Itoa(schemaVersion) + "|" + schemaDigestHex,
		"function/runtime_event_ordering_key": strings.Join([]string{
			"jsonb",
			"text, text, text, text",
			"i",
			"s",
			"0",
			"0",
			"search_path=pg_catalog",
			"SELECTCASEWHENorganization_idISNULLTHEN" +
				"jsonb_build_array(event_name,aggregate_type,aggregate_id)" +
				"ELSEjsonb_build_array(organization_id,event_name," +
				"aggregate_type,aggregate_id)END",
		}, "|"),
		"table/runtime_event_schema_versions":     "r|p|0|0|0|0|0|0|0|0|d|heap",
		"table/runtime_event_cursors":             "r|p|0|0|0|0|0|0|0|0|d|heap",
		"table/runtime_inbox_events":              "r|p|0|0|0|0|0|0|0|0|d|heap",
		"table/runtime_inbox_repairs":             "r|p|0|0|0|0|0|0|0|0|d|heap",
		"privilege/runtime_event_schema_versions": "1",
		"privilege/runtime_event_cursors":         "1",
		"privilege/runtime_inbox_events":          "1",
		"privilege/runtime_inbox_repairs":         "1",
		"privilege/runtime_event_ordering_key":    "1",
		"privilege/schema":                        "1",
		"privilege/principal":                     "1",
		"privilege/sequences":                     "1",
	}

	addRequiredColumns(required)
	addRequiredConstraints(required)
	addRequiredIndexes(required)
	return required
}

func addRequiredColumns(required map[string]string) {
	columns := map[string]string{
		"runtime_event_schema_versions.component":     columnSignature("text", true, "-", "-"),
		"runtime_event_schema_versions.version":       columnSignature("integer", true, "-", "-"),
		"runtime_event_schema_versions.schema_digest": columnSignature("bytea", true, "-", "-"),
		"runtime_event_schema_versions.installed_at":  columnSignature("timestamp with time zone", true, "-", "clock_timestamp"),

		"runtime_event_cursors.consumer_name":     columnSignature("text", true, "-", "-"),
		"runtime_event_cursors.consumer_scope":    columnSignature("text", true, "-", "-"),
		"runtime_event_cursors.ordering_key":      columnSignature("jsonb", true, "-", "-"),
		"runtime_event_cursors.last_sequence":     columnSignature("bigint", true, "-", "0"),
		"runtime_event_cursors.last_event_id":     columnSignature("uuid", false, "-", "-"),
		"runtime_event_cursors.last_event_digest": columnSignature("bytea", false, "-", "-"),
		"runtime_event_cursors.next_fence":        columnSignature("bigint", true, "-", "1"),
		"runtime_event_cursors.updated_at":        columnSignature("timestamp with time zone", true, "-", "clock_timestamp"),

		"runtime_inbox_events.consumer_name":     columnSignature("text", true, "-", "-"),
		"runtime_inbox_events.consumer_scope":    columnSignature("text", true, "-", "-"),
		"runtime_inbox_events.event_id":          columnSignature("uuid", true, "-", "-"),
		"runtime_inbox_events.event_digest":      columnSignature("bytea", true, "-", "-"),
		"runtime_inbox_events.event_name":        columnSignature("text", true, "-", "-"),
		"runtime_inbox_events.event_version":     columnSignature("integer", true, "-", "-"),
		"runtime_inbox_events.schema_version":    columnSignature("integer", true, "-", "-"),
		"runtime_inbox_events.occurred_at":       columnSignature("timestamp with time zone", true, "-", "-"),
		"runtime_inbox_events.organization_id":   columnSignature("text", false, "-", "-"),
		"runtime_inbox_events.aggregate_type":    columnSignature("text", true, "-", "-"),
		"runtime_inbox_events.aggregate_id":      columnSignature("text", true, "-", "-"),
		"runtime_inbox_events.aggregate_version": columnSignature("bigint", true, "-", "-"),
		"runtime_inbox_events.event_sequence":    columnSignature("bigint", true, "-", "-"),
		"runtime_inbox_events.ordering_key":      columnSignature("jsonb", true, "s", "runtime_event_ordering_keyorganization_id,event_name,aggregate_type,aggregate_id"),
		"runtime_inbox_events.state":             columnSignature("text", true, "-", "-"),
		"runtime_inbox_events.attempts":          columnSignature("integer", true, "-", "0"),
		"runtime_inbox_events.max_attempts":      columnSignature("integer", true, "-", "-"),
		"runtime_inbox_events.repair_count":      columnSignature("integer", true, "-", "0"),
		"runtime_inbox_events.max_repairs":       columnSignature("integer", true, "-", "-"),
		"runtime_inbox_events.lease_owner":       columnSignature("text", false, "-", "-"),
		"runtime_inbox_events.lease_token":       columnSignature("uuid", false, "-", "-"),
		"runtime_inbox_events.lease_generation":  columnSignature("bigint", true, "-", "0"),
		"runtime_inbox_events.lease_fence":       columnSignature("bigint", true, "-", "0"),
		"runtime_inbox_events.lease_expires_at":  columnSignature("timestamp with time zone", false, "-", "-"),
		"runtime_inbox_events.available_at":      columnSignature("timestamp with time zone", true, "-", "clock_timestamp"),
		"runtime_inbox_events.last_error_code":   columnSignature("text", false, "-", "-"),
		"runtime_inbox_events.received_at":       columnSignature("timestamp with time zone", true, "-", "clock_timestamp"),
		"runtime_inbox_events.updated_at":        columnSignature("timestamp with time zone", true, "-", "clock_timestamp"),
		"runtime_inbox_events.processed_at":      columnSignature("timestamp with time zone", false, "-", "-"),
		"runtime_inbox_events.cleanup_after":     columnSignature("timestamp with time zone", false, "-", "-"),
		"runtime_inbox_events.terminal_at":       columnSignature("timestamp with time zone", false, "-", "-"),

		"runtime_inbox_repairs.consumer_name":       columnSignature("text", true, "-", "-"),
		"runtime_inbox_repairs.consumer_scope":      columnSignature("text", true, "-", "-"),
		"runtime_inbox_repairs.organization_scope":  columnSignature("text", true, "-", "-"),
		"runtime_inbox_repairs.project_scope":       columnSignature("text", true, "-", "-"),
		"runtime_inbox_repairs.operation":           columnSignature("text", true, "-", "-"),
		"runtime_inbox_repairs.key_hash":            columnSignature("bytea", true, "-", "-"),
		"runtime_inbox_repairs.request_digest":      columnSignature("bytea", true, "-", "-"),
		"runtime_inbox_repairs.receipt_id":          columnSignature("uuid", true, "-", "-"),
		"runtime_inbox_repairs.event_id":            columnSignature("uuid", true, "-", "-"),
		"runtime_inbox_repairs.event_digest":        columnSignature("bytea", true, "-", "-"),
		"runtime_inbox_repairs.expected_generation": columnSignature("bigint", true, "-", "-"),
		"runtime_inbox_repairs.expected_fence":      columnSignature("bigint", true, "-", "-"),
		"runtime_inbox_repairs.action":              columnSignature("text", true, "-", "-"),
		"runtime_inbox_repairs.actor":               columnSignature("text", true, "-", "-"),
		"runtime_inbox_repairs.reason":              columnSignature("text", true, "-", "-"),
		"runtime_inbox_repairs.evidence_digest":     columnSignature("bytea", true, "-", "-"),
		"runtime_inbox_repairs.result_generation":   columnSignature("bigint", true, "-", "-"),
		"runtime_inbox_repairs.result_fence":        columnSignature("bigint", true, "-", "-"),
		"runtime_inbox_repairs.result_directive":    columnSignature("text", true, "-", "-"),
		"runtime_inbox_repairs.created_at":          columnSignature("timestamp with time zone", true, "-", "clock_timestamp"),
	}
	for name, signature := range columns {
		required["column/"+name] = signature
	}
}

func columnSignature(typeName string, notNull bool, generated, defaultExpression string) string {
	notNullValue := "0"
	if notNull {
		notNullValue = "1"
	}
	return strings.Join([]string{typeName, notNullValue, "-", generated, defaultExpression}, "|")
}

func addRequiredConstraints(required map[string]string) {
	constraints := map[string]string{
		"runtime_event_schema_versions.runtime_event_schema_versions_pkey":            "p|1|0|0|1|PRIMARYKEYcomponent",
		"runtime_event_schema_versions.runtime_event_schema_versions_component_check": "c|1|0|0|-|CHECKcomponent~'^[a-z][a-z0-9_]{0,62}$'::text",
		"runtime_event_schema_versions.runtime_event_schema_versions_version_check":   "c|1|0|0|-|CHECKversion>0",
		"runtime_event_schema_versions.runtime_event_schema_versions_digest_check":    "c|1|0|0|-|CHECKoctet_lengthschema_digest=32",

		"runtime_event_cursors.runtime_event_cursors_pkey":                 "p|1|0|0|1|PRIMARYKEYconsumer_name,consumer_scope,ordering_key",
		"runtime_event_cursors.runtime_event_cursors_consumer_name_check":  "c|1|0|0|-|CHECKconsumer_name~'^[a-z][a-z0-9._-]{0,127}$'::text",
		"runtime_event_cursors.runtime_event_cursors_consumer_scope_check": "c|1|0|0|-|CHECKconsumer_scope~'^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$'::text",
		"runtime_event_cursors.runtime_event_cursors_ordering_key_check":   "c|1|0|0|-|CHECKjsonb_typeofordering_key='array'::textANDjsonb_array_lengthordering_key=ANYARRAY[3,4]ANDoctet_lengthordering_key::text>=7ANDoctet_lengthordering_key::text<=1024",
		"runtime_event_cursors.runtime_event_cursors_sequence_check":       "c|1|0|0|-|CHECKlast_sequence>=0",
		"runtime_event_cursors.runtime_event_cursors_fence_check":          "c|1|0|0|-|CHECKnext_fence>0",
		"runtime_event_cursors.runtime_event_cursors_high_watermark_check": "c|1|0|0|-|CHECKlast_sequence=0ANDlast_event_idISNULLANDlast_event_digestISNULLORlast_sequence>0ANDlast_event_idISNOTNULLANDoctet_lengthlast_event_digest=32",

		"runtime_inbox_events.runtime_inbox_events_pkey":                       "p|1|0|0|1|PRIMARYKEYconsumer_name,consumer_scope,event_id",
		"runtime_inbox_events.runtime_inbox_events_sequence_key":               "u|1|0|0|1|UNIQUEconsumer_name,consumer_scope,ordering_key,event_sequence",
		"runtime_inbox_events.runtime_inbox_events_consumer_name_check":        "c|1|0|0|-|CHECKconsumer_name~'^[a-z][a-z0-9._-]{0,127}$'::text",
		"runtime_inbox_events.runtime_inbox_events_consumer_scope_check":       "c|1|0|0|-|CHECKconsumer_scope~'^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$'::text",
		"runtime_inbox_events.runtime_inbox_events_digest_check":               "c|1|0|0|-|CHECKoctet_lengthevent_digest=32",
		"runtime_inbox_events.runtime_inbox_events_event_name_check":           "c|1|0|0|-|CHECKchar_lengthevent_name>=1ANDchar_lengthevent_name<=128",
		"runtime_inbox_events.runtime_inbox_events_event_version_check":        "c|1|0|0|-|CHECKevent_version>0",
		"runtime_inbox_events.runtime_inbox_events_schema_version_check":       "c|1|0|0|-|CHECKschema_version>0",
		"runtime_inbox_events.runtime_inbox_events_organization_check":         "c|1|0|0|-|CHECKorganization_idISNULLORchar_lengthorganization_id>=1ANDchar_lengthorganization_id<=128",
		"runtime_inbox_events.runtime_inbox_events_aggregate_type_check":       "c|1|0|0|-|CHECKchar_lengthaggregate_type>=1ANDchar_lengthaggregate_type<=64",
		"runtime_inbox_events.runtime_inbox_events_aggregate_id_check":         "c|1|0|0|-|CHECKchar_lengthaggregate_id>=1ANDchar_lengthaggregate_id<=128",
		"runtime_inbox_events.runtime_inbox_events_aggregate_version_check":    "c|1|0|0|-|CHECKaggregate_version>0",
		"runtime_inbox_events.runtime_inbox_events_event_sequence_check":       "c|1|0|0|-|CHECKevent_sequence>0",
		"runtime_inbox_events.runtime_inbox_events_ordering_key_check":         "c|1|0|0|-|CHECKjsonb_typeofordering_key='array'::textANDjsonb_array_lengthordering_key=ANYARRAY[3,4]ANDoctet_lengthordering_key::text>=7ANDoctet_lengthordering_key::text<=1024",
		"runtime_inbox_events.runtime_inbox_events_state_check":                "c|1|0|0|-|CHECKstate=ANYARRAY['RECEIVED'::text,'PROCESSING'::text,'RETRY'::text,'COMPLETED'::text,'STALE'::text,'DEAD_LETTER'::text]",
		"runtime_inbox_events.runtime_inbox_events_attempt_budget_check":       "c|1|0|0|-|CHECKattempts>=0ANDmax_attempts>=1ANDmax_attempts<=100ANDattempts<=max_attempts",
		"runtime_inbox_events.runtime_inbox_events_repair_budget_check":        "c|1|0|0|-|CHECKrepair_count>=0ANDmax_repairs>=1ANDmax_repairs<=20ANDrepair_count<=max_repairs",
		"runtime_inbox_events.runtime_inbox_events_lease_generation_check":     "c|1|0|0|-|CHECKlease_generation>=0ANDlease_generation=0=lease_fence=0",
		"runtime_inbox_events.runtime_inbox_events_lease_fence_check":          "c|1|0|0|-|CHECKlease_fence>=0",
		"runtime_inbox_events.runtime_inbox_events_error_code_check":           "c|1|0|0|-|CHECKlast_error_codeISNULLORlast_error_code~'^[a-z][a-z0-9_]{0,62}$'::text",
		"runtime_inbox_events.runtime_inbox_events_lease_consistency_check":    "c|1|0|0|-|CHECKstate='PROCESSING'::textANDlease_ownerISNOTNULLANDchar_lengthlease_owner>=1ANDchar_lengthlease_owner<=128ANDlease_tokenISNOTNULLANDlease_generation>0ANDlease_fence>0ANDlease_expires_atISNOTNULLORstate<>'PROCESSING'::textANDlease_ownerISNULLANDlease_tokenISNULLANDlease_expires_atISNULL",
		"runtime_inbox_events.runtime_inbox_events_terminal_consistency_check": "c|1|0|0|-|CHECKstate=ANYARRAY['COMPLETED'::text,'STALE'::text]ANDprocessed_atISNOTNULLANDcleanup_afterISNOTNULLANDterminal_atISNULLORstate='DEAD_LETTER'::textANDprocessed_atISNULLANDcleanup_afterISNULLANDterminal_atISNOTNULLORstate=ANYARRAY['RECEIVED'::text,'PROCESSING'::text,'RETRY'::text]ANDprocessed_atISNULLANDcleanup_afterISNULLANDterminal_atISNULL",

		"runtime_inbox_repairs.runtime_inbox_repairs_pkey":                   "p|1|0|0|1|PRIMARYKEYorganization_scope,project_scope,operation,key_hash",
		"runtime_inbox_repairs.runtime_inbox_repairs_receipt_id_key":         "u|1|0|0|1|UNIQUEreceipt_id",
		"runtime_inbox_repairs.runtime_inbox_repairs_consumer_name_check":    "c|1|0|0|-|CHECKconsumer_name~'^[a-z][a-z0-9._-]{0,127}$'::text",
		"runtime_inbox_repairs.runtime_inbox_repairs_consumer_scope_check":   "c|1|0|0|-|CHECKconsumer_scope~'^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$'::text",
		"runtime_inbox_repairs.runtime_inbox_repairs_authorized_scope_check": "c|1|0|0|-|CHECKorganization_scope~'^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$'::textANDproject_scope~'^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$'::textANDoperation~'^[a-z][a-z0-9._-]{0,127}$'::textANDoctet_lengthkey_hash=32",
		"runtime_inbox_repairs.runtime_inbox_repairs_request_digest_check":   "c|1|0|0|-|CHECKoctet_lengthrequest_digest=32",
		"runtime_inbox_repairs.runtime_inbox_repairs_event_digest_check":     "c|1|0|0|-|CHECKoctet_lengthevent_digest=32",
		"runtime_inbox_repairs.runtime_inbox_repairs_expected_fence_check":   "c|1|0|0|-|CHECKexpected_generation>=0ANDexpected_fence>=0ANDexpected_generation=0=expected_fence=0",
		"runtime_inbox_repairs.runtime_inbox_repairs_action_check":           "c|1|0|0|-|CHECKaction=ANYARRAY['REQUEUE'::text,'REJOIN'::text,'TERMINALIZE'::text,'WAIT'::text]",
		"runtime_inbox_repairs.runtime_inbox_repairs_actor_check":            "c|1|0|0|-|CHECKchar_lengthactor>=1ANDchar_lengthactor<=256",
		"runtime_inbox_repairs.runtime_inbox_repairs_reason_check":           "c|1|0|0|-|CHECKchar_lengthreason>=1ANDchar_lengthreason<=1024",
		"runtime_inbox_repairs.runtime_inbox_repairs_evidence_digest_check":  "c|1|0|0|-|CHECKoctet_lengthevidence_digest=32",
		"runtime_inbox_repairs.runtime_inbox_repairs_result_fence_check":     "c|1|0|0|-|CHECKresult_generation>=0ANDresult_fence>=0ANDresult_generation=expected_generationANDresult_fence=expected_fence",
		"runtime_inbox_repairs.runtime_inbox_repairs_result_directive_check": "c|1|0|0|-|CHECKresult_directive=ANYARRAY['replay_required'::text,'wait_predecessor'::text,'wait_lease'::text,'wait_backoff'::text,'repair_required'::text,'ack_eligible'::text]",
	}
	for name, signature := range constraints {
		required["constraint/"+name] = signature
	}
}

func addRequiredIndexes(required map[string]string) {
	indexes := map[string]string{
		"runtime_inbox_events_claim_idx": indexSignature(
			5,
			"consumer_name,consumer_scope,available_at,occurred_at,event_id",
			"state=ANYARRAY['RECEIVED'::text,'RETRY'::text]",
			"CREATEINDEXruntime_inbox_events_claim_idxONruntime_inbox_eventsUSINGbtreeconsumer_name,consumer_scope,available_at,occurred_at,event_idWHEREstate=ANYARRAY['RECEIVED'::text,'RETRY'::text]",
		),
		"runtime_inbox_events_lease_idx": indexSignature(
			4,
			"lease_expires_at,consumer_name,consumer_scope,event_id",
			"state='PROCESSING'::text",
			"CREATEINDEXruntime_inbox_events_lease_idxONruntime_inbox_eventsUSINGbtreelease_expires_at,consumer_name,consumer_scope,event_idWHEREstate='PROCESSING'::text",
		),
		"runtime_inbox_events_ordering_idx": indexSignature(
			4,
			"consumer_name,consumer_scope,ordering_key,event_sequence",
			"state=ANYARRAY['RECEIVED'::text,'PROCESSING'::text,'RETRY'::text,'DEAD_LETTER'::text]",
			"CREATEINDEXruntime_inbox_events_ordering_idxONruntime_inbox_eventsUSINGbtreeconsumer_name,consumer_scope,ordering_key,event_sequenceWHEREstate=ANYARRAY['RECEIVED'::text,'PROCESSING'::text,'RETRY'::text,'DEAD_LETTER'::text]",
		),
		"runtime_inbox_events_retention_idx": indexSignature(
			4,
			"cleanup_after,consumer_name,consumer_scope,event_id",
			"state=ANYARRAY['COMPLETED'::text,'STALE'::text]",
			"CREATEINDEXruntime_inbox_events_retention_idxONruntime_inbox_eventsUSINGbtreecleanup_after,consumer_name,consumer_scope,event_idWHEREstate=ANYARRAY['COMPLETED'::text,'STALE'::text]",
		),
		"runtime_inbox_events_dead_letter_idx": indexSignature(
			4,
			"consumer_name,consumer_scope,terminal_at,event_id",
			"state='DEAD_LETTER'::text",
			"CREATEINDEXruntime_inbox_events_dead_letter_idxONruntime_inbox_eventsUSINGbtreeconsumer_name,consumer_scope,terminal_at,event_idWHEREstate='DEAD_LETTER'::text",
		),
		"runtime_inbox_repairs_event_idx": indexSignature(
			5,
			"consumer_name,consumer_scope,event_id,created_at,receipt_id",
			"-",
			"CREATEINDEXruntime_inbox_repairs_event_idxONruntime_inbox_repairsUSINGbtreeconsumer_name,consumer_scope,event_id,created_at,receipt_id",
		),
	}
	for name, signature := range indexes {
		required["index/"+name] = signature
	}
}

func indexSignature(keyCount int, keys, predicate, definition string) string {
	return strings.Join([]string{
		"0", // unique
		"0", // primary
		"0", // exclusion
		"1", // immediate
		"1", // valid
		"1", // ready
		"1", // live
		"0", // replica identity
		"0", // NULLS NOT DISTINCT
		strconv.Itoa(keyCount),
		strconv.Itoa(keyCount),
		"btree",
		keys,
		predicate,
		definition,
	}, "|")
}
