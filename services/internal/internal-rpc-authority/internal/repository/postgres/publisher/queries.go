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

func validateQueries() error {
	for name, query := range map[string]string{
		"publisher__load_delivery": loadDeliverySQL,
		"publisher__save_delivery": saveDeliverySQL,
		"publisher__readiness":     readinessSQL,
	} {
		if strings.TrimSpace(query) == "" ||
			!strings.HasPrefix(strings.TrimSpace(query), "-- name: "+name+" ") {
			return errors.New("invalid embedded publisher query")
		}
	}
	return nil
}
