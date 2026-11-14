package integration

import (
	"errors"
	"strings"
	"testing"
)

// TestObservabilityOfficialLogQLAST qualifies real upstream parsing for logs, JSON/logfmt pipelines, regex filters, unwrap, aggregations, binary comparisons and label replacement with PromQL0.314 in the same binary.
// TestObservabilityOfficialLogQLAST квалифицирует настоящий upstream parsing logs, JSON/logfmt pipelines, regex filters, unwrap, aggregations, binary comparisons и label replacement вместе с PromQL0.314 в одном binary.
func TestObservabilityOfficialLogQLAST(t *testing.T) {
	for _, query := range []string{`{app="fixture"}`, `{app="fixture"} |= "error"`, `{app="fixture"} |~ "timeout|failure"`, `{app="fixture"} | json | level="error"`, `{app="fixture"} | logfmt | duration > 1s`} {
		if _, err := validateObservabilityLogQL(query, "logs"); err != nil {
			t.Fatalf("documented bounded log pipeline rejected: %s", query)
		}
	}
	for _, query := range []string{`sum(rate({app="fixture"}[5m]))`, `sum by (job)(count_over_time({app="fixture"} |= "error"[10m])) > 5`, `avg_over_time({app="fixture"} | logfmt | unwrap duration [5m])`, `label_replace(sum(rate({app="fixture"}[5m])),"service","$1","job","(.*)")`, `vector(1)`} {
		if _, err := validateObservabilityLogQL(query, "metric"); err != nil {
			t.Fatalf("documented bounded metric pipeline rejected: %s", query)
		}
	}
	if _, err := validateObservabilityLogQL(`{app="fixture",job=~"api.+"}`, "selector"); err != nil {
		t.Fatal("bounded native selector rejected", err)
	}
}

// TestObservabilityLogQLBudgetRejectsBeforeSemanticUse checks parser failures, wrong result kinds, explicit unsupported template stages, excessive history, selectors, depth and regex size without sending HTTP.
// TestObservabilityLogQLBudgetRejectsBeforeSemanticUse проверяет parser failures, wrong result kinds, явные unsupported template stages, excessive history, selectors, depth и regex size без HTTP.
func TestObservabilityLogQLBudgetRejectsBeforeSemanticUse(t *testing.T) {
	for _, test := range []struct{ query, kind string }{
		{`{app="fixture"`, "logs"}, {`{app="fixture"}`, "metric"}, {`sum(rate({app="fixture"}[5m]))`, "logs"}, {`{app="fixture"} |= "x"`, "selector"},
		{`rate({app="fixture"}[100y])`, "metric"}, {`rate({app="fixture"}[23h] offset 2h)`, "metric"}, {`{app="fixture"} |~ "` + strings.Repeat("a", 257) + `"`, "logs"},
		{`{app="fixture"}` + strings.Repeat(` |= "x"`, 40), "logs"},
	} {
		if _, err := validateObservabilityLogQL(test.query, test.kind); err == nil {
			t.Fatal("unbounded/malformed or mismatched LogQL accepted")
		}
	}
	for _, query := range []string{`{app="fixture"} | line_format "{{.message}}"`, `{app="fixture"} | label_format service="{{.app}}"`} {
		_, err := validateObservabilityLogQL(query, "logs")
		var safe *SafeError
		if !errors.As(err, &safe) || safe.Code != queryProfileUnsupportedCode {
			t.Fatal("formatting allocation profile lacks an explicit unsupported reason", err)
		}
	}
}
