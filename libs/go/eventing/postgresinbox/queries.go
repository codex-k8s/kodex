package postgresinbox

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed sql/schema__set_search_path.sql
var rawSchemaSetSearchPath string

//go:embed sql/cursor__ensure.sql
var rawCursorEnsure string

//go:embed sql/cursor__get_for_update.sql
var rawCursorGetForUpdate string

//go:embed sql/cursor__take_fence.sql
var rawCursorTakeFence string

//go:embed sql/cursor__advance.sql
var rawCursorAdvance string

//go:embed sql/inbox__get_by_event_for_update.sql
var rawInboxGetByEventForUpdate string

//go:embed sql/inbox__get_by_event.sql
var rawInboxGetByEvent string

//go:embed sql/inbox__get_by_sequence_for_update.sql
var rawInboxGetBySequenceForUpdate string

//go:embed sql/inbox__insert_received.sql
var rawInboxInsertReceived string

//go:embed sql/inbox__insert_stale.sql
var rawInboxInsertStale string

//go:embed sql/inbox__claim.sql
var rawInboxClaim string

//go:embed sql/inbox__complete.sql
var rawInboxComplete string

//go:embed sql/inbox__mark_retry.sql
var rawInboxMarkRetry string

//go:embed sql/inbox__mark_dead_letter.sql
var rawInboxMarkDeadLetter string

//go:embed sql/inbox__expire_to_dead_letter.sql
var rawInboxExpireToDeadLetter string

//go:embed sql/inbox__renew.sql
var rawInboxRenew string

//go:embed sql/inbox__cleanup.sql
var rawInboxCleanup string

//go:embed sql/operator__get_receipt.sql
var rawOperatorGetReceipt string

//go:embed sql/operator__insert_receipt.sql
var rawOperatorInsertReceipt string

//go:embed sql/inbox__requeue.sql
var rawInboxRequeue string

//go:embed sql/inbox__recover_rejoin.sql
var rawInboxRecoverRejoin string

//go:embed sql/blockage__get.sql
var rawBlockageGet string

//go:embed sql/blockage__list.sql
var rawBlockageList string

//go:embed sql/delivery__read_outcome.sql
var rawDeliveryReadOutcome string

//go:embed sql/effect__inspect.sql
var rawEffectInspect string

//go:embed sql/effect__call.sql
var rawEffectCall string

//go:embed sql/schema__inspect.sql
var rawSchemaInspect string

//go:embed sql/schema__probe.sql
var rawSchemaProbe string

type querySet struct {
	schemaSetSearchPath         string
	cursorEnsure                string
	cursorGetForUpdate          string
	cursorTakeFence             string
	cursorAdvance               string
	inboxGetByEventForUpdate    string
	inboxGetByEvent             string
	inboxGetBySequenceForUpdate string
	inboxInsertReceived         string
	inboxInsertStale            string
	inboxClaim                  string
	inboxComplete               string
	inboxMarkRetry              string
	inboxMarkDeadLetter         string
	inboxExpireToDeadLetter     string
	inboxRenew                  string
	inboxCleanup                string
	operatorGetReceipt          string
	operatorInsertReceipt       string
	inboxRequeue                string
	inboxRecoverRejoin          string
	blockageGet                 string
	blockageList                string
	deliveryReadOutcome         string
	effectInspect               string
	schemaInspect               string
	schemaProbe                 string
}

func loadQueries() (querySet, error) {
	definitions := []struct {
		filename    string
		name        string
		cardinality string
		raw         string
	}{
		{"schema__set_search_path.sql", "schema__set_search_path", ":one", rawSchemaSetSearchPath},
		{"cursor__ensure.sql", "cursor__ensure", ":exec", rawCursorEnsure},
		{"cursor__get_for_update.sql", "cursor__get_for_update", ":one", rawCursorGetForUpdate},
		{"cursor__take_fence.sql", "cursor__take_fence", ":one", rawCursorTakeFence},
		{"cursor__advance.sql", "cursor__advance", ":exec", rawCursorAdvance},
		{"inbox__get_by_event_for_update.sql", "inbox__get_by_event_for_update", ":one", rawInboxGetByEventForUpdate},
		{"inbox__get_by_event.sql", "inbox__get_by_event", ":one", rawInboxGetByEvent},
		{"inbox__get_by_sequence_for_update.sql", "inbox__get_by_sequence_for_update", ":one", rawInboxGetBySequenceForUpdate},
		{"inbox__insert_received.sql", "inbox__insert_received", ":one", rawInboxInsertReceived},
		{"inbox__insert_stale.sql", "inbox__insert_stale", ":one", rawInboxInsertStale},
		{"inbox__claim.sql", "inbox__claim", ":one", rawInboxClaim},
		{"inbox__complete.sql", "inbox__complete", ":exec", rawInboxComplete},
		{"inbox__mark_retry.sql", "inbox__mark_retry", ":exec", rawInboxMarkRetry},
		{"inbox__mark_dead_letter.sql", "inbox__mark_dead_letter", ":exec", rawInboxMarkDeadLetter},
		{"inbox__expire_to_dead_letter.sql", "inbox__expire_to_dead_letter", ":exec", rawInboxExpireToDeadLetter},
		{"inbox__renew.sql", "inbox__renew", ":one", rawInboxRenew},
		{"inbox__cleanup.sql", "inbox__cleanup", ":one", rawInboxCleanup},
		{"operator__get_receipt.sql", "operator__get_receipt", ":one", rawOperatorGetReceipt},
		{"operator__insert_receipt.sql", "operator__insert_receipt", ":one", rawOperatorInsertReceipt},
		{"inbox__requeue.sql", "inbox__requeue", ":exec", rawInboxRequeue},
		{"inbox__recover_rejoin.sql", "inbox__recover_rejoin", ":exec", rawInboxRecoverRejoin},
		{"blockage__get.sql", "blockage__get", ":one", rawBlockageGet},
		{"blockage__list.sql", "blockage__list", ":many", rawBlockageList},
		{"delivery__read_outcome.sql", "delivery__read_outcome", ":one", rawDeliveryReadOutcome},
		{"effect__inspect.sql", "effect__inspect", ":one", rawEffectInspect},
		{"schema__inspect.sql", "schema__inspect", ":many", rawSchemaInspect},
		{"schema__probe.sql", "schema__probe", ":one", rawSchemaProbe},
	}

	queries := querySet{}
	targets := []*string{
		&queries.schemaSetSearchPath,
		&queries.cursorEnsure,
		&queries.cursorGetForUpdate,
		&queries.cursorTakeFence,
		&queries.cursorAdvance,
		&queries.inboxGetByEventForUpdate,
		&queries.inboxGetByEvent,
		&queries.inboxGetBySequenceForUpdate,
		&queries.inboxInsertReceived,
		&queries.inboxInsertStale,
		&queries.inboxClaim,
		&queries.inboxComplete,
		&queries.inboxMarkRetry,
		&queries.inboxMarkDeadLetter,
		&queries.inboxExpireToDeadLetter,
		&queries.inboxRenew,
		&queries.inboxCleanup,
		&queries.operatorGetReceipt,
		&queries.operatorInsertReceipt,
		&queries.inboxRequeue,
		&queries.inboxRecoverRejoin,
		&queries.blockageGet,
		&queries.blockageList,
		&queries.deliveryReadOutcome,
		&queries.effectInspect,
		&queries.schemaInspect,
		&queries.schemaProbe,
	}
	if len(definitions) != len(targets) {
		return querySet{}, ErrInvalidConfiguration
	}
	for index := range definitions {
		loaded, err := loadQuery(
			definitions[index].filename,
			definitions[index].name,
			definitions[index].cardinality,
			definitions[index].raw,
		)
		if err != nil {
			return querySet{}, err
		}
		*targets[index] = loaded
	}
	return queries, nil
}

func loadQuery(filename, name, cardinality, raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	lines := strings.SplitN(trimmed, "\n", 2)
	expectedHeader := fmt.Sprintf("-- name: %s %s", name, cardinality)
	if len(lines) != 2 || strings.TrimSpace(lines[0]) != expectedHeader ||
		strings.TrimSpace(lines[1]) == "" || filename != name+".sql" {
		return "", ErrInvalidConfiguration
	}
	return trimmed, nil
}

func buildEffectCallQuery(identifier string) (string, error) {
	template, err := loadQuery(
		"effect__call.sql",
		"effect__call",
		":one",
		rawEffectCall,
	)
	if err != nil || strings.Count(template, "__postgresinbox_effect_function__") != 1 {
		return "", ErrInvalidConfiguration
	}
	return strings.Replace(
		template,
		"__postgresinbox_effect_function__",
		identifier,
		1,
	), nil
}
