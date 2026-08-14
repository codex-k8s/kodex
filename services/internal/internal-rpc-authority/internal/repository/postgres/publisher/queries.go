package publisher

import (
	_ "embed"
	"errors"
	"strings"
)

//go:embed sql/publisher__load_delivery.sql
var loadDeliverySQL string

//go:embed sql/publisher__save_delivery.sql
var saveDeliverySQL string

//go:embed sql/publisher__readiness.sql
var readinessSQL string

//go:embed sql/publisher__pin_readback_intent.sql
var pinReadbackIntentSQL string

//go:embed sql/publisher__load_pinned_readback_intent.sql
var loadPinnedReadbackIntentSQL string

//go:embed sql/publisher__load_snapshot_history.sql
var loadSnapshotHistorySQL string

//go:embed sql/publisher__load_snapshot_publication.sql
var loadSnapshotPublicationSQL string

//go:embed sql/publisher__append_snapshot.sql
var appendSnapshotSQL string

//go:embed sql/publisher__promote_snapshot.sql
var promoteSnapshotSQL string

func validateQueries() error {
	for name, query := range map[string]string{
		"publisher__load_delivery":               loadDeliverySQL,
		"publisher__save_delivery":               saveDeliverySQL,
		"publisher__readiness":                   readinessSQL,
		"publisher__pin_readback_intent":         pinReadbackIntentSQL,
		"publisher__load_pinned_readback_intent": loadPinnedReadbackIntentSQL,
		"publisher__load_snapshot_history":       loadSnapshotHistorySQL,
		"publisher__load_snapshot_publication":   loadSnapshotPublicationSQL,
		"publisher__append_snapshot":             appendSnapshotSQL,
		"publisher__promote_snapshot":            promoteSnapshotSQL,
	} {
		if strings.TrimSpace(query) == "" ||
			!strings.HasPrefix(strings.TrimSpace(query), "-- name: "+name+" ") {
			return errors.New("invalid embedded publisher query")
		}
	}
	return nil
}
