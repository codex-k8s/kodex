package readback

import (
	_ "embed"
	"errors"
	"strings"
)

//go:embed sql/readback__resolve_intent.sql
var resolveIntentSQL string

//go:embed sql/readback__issue_challenge.sql
var issueChallengeSQL string

//go:embed sql/readback__load_challenge.sql
var loadChallengeSQL string

//go:embed sql/readback__load_receipt.sql
var loadReceiptSQL string

//go:embed sql/readback__consume_challenge.sql
var consumeChallengeSQL string

//go:embed sql/readback__readiness.sql
var readinessSQL string

func validateQueries() error {
	for name, query := range map[string]string{
		"readback__resolve_intent":    resolveIntentSQL,
		"readback__issue_challenge":   issueChallengeSQL,
		"readback__load_challenge":    loadChallengeSQL,
		"readback__load_receipt":      loadReceiptSQL,
		"readback__consume_challenge": consumeChallengeSQL,
		"readback__readiness":         readinessSQL,
	} {
		if strings.TrimSpace(query) == "" ||
			!strings.HasPrefix(strings.TrimSpace(query), "-- name: "+name+" ") {
			return errors.New("invalid embedded readback query")
		}
	}
	return nil
}
