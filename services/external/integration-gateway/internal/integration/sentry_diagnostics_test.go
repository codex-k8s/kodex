package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestSentryEventDiagnosticsConsumer exercises native camel-case frame coordinates through Execute, preserving bounded useful diagnostics while dropping request/variable/source data and refusing foreign project or selected-key echoes.
// TestSentryEventDiagnosticsConsumer проверяет native camel-case frame coordinates через Execute, сохраняя bounded useful diagnostics, исключая request/variable/source data и отклоняя foreign project либо отражение выбранного ключа.
func TestSentryEventDiagnosticsConsumer(t *testing.T) {
	for _, scenario := range []string{"normal", "truncated", "foreign-project", "selected-key", "nullable-frame", "null-frame"} {
		t.Run(scenario, func(t *testing.T) {
			adapter := testAdapter(t)
			input := map[string]any{"event_id": strings.Repeat("a", 32)}
			request := sentryInvocation(t, adapter, "sentry", "sentry.event.read", input)
			frames := []any{}
			frameCount, exceptionCount := 1, 1
			if scenario == "truncated" {
				frameCount, exceptionCount = 40, 4
			}
			for index := 0; index < frameCount; index++ {
				frame := map[string]any{"filename": "app.go", "function": fmt.Sprintf("frame-%02d", index), "module": "example.invalid/application", "lineNo": 42, "colNo": 7, "inApp": true, "vars": map[string]string{"password": "DROP_VARIABLE"}, "context": []any{[]any{42, "DROP_SOURCE"}}}
				if scenario == "truncated" {
					frame["filename"] = strings.Repeat("Ж", 50)
				}
				if scenario == "nullable-frame" {
					for _, key := range []string{"filename", "function", "module", "lineNo", "colNo", "inApp"} {
						frame[key] = nil
					}
				}
				frames = append(frames, frame)
			}
			if scenario == "null-frame" {
				frames = []any{nil}
			}
			exceptions := []any{}
			for index := 0; index < exceptionCount; index++ {
				value := "Synthetic failure"
				if scenario == "selected-key" {
					value = "synthetic-sentry-token"
				}
				exceptions = append(exceptions, map[string]any{"type": "ValueError", "value": value, "stacktrace": map[string]any{"frames": frames}})
			}
			projectID := "3"
			if scenario == "foreign-project" {
				projectID = "4"
			}
			event := map[string]any{"eventID": input["event_id"], "projectID": projectID, "title": "Synthetic error", "platform": "go", "entries": []any{map[string]any{"type": "exception", "data": map[string]any{"values": exceptions}}, map[string]any{"type": "request", "data": map[string]string{"cookies": "DROP_COOKIE"}}, map[string]any{"type": "breadcrumbs", "data": map[string]string{"message": "DROP_BREADCRUMB"}}}}
			raw, _ := json.Marshal(event)
			if len(raw) > maximumResponseBytes {
				t.Fatal("diagnostic fixture exceeds the bounded native response profile")
			}
			adapter.openAPIHTTPClient = &http.Client{Transport: roundTripFunc(func(wire *http.Request) (*http.Response, error) {
				response := sentryWireFixture(t, wire)
				if strings.Contains(wire.URL.Path, "/events/") {
					response.Body = io.NopCloser(strings.NewReader(string(raw)))
				}
				return response, nil
			})}
			result, err := adapter.Execute(t.Context(), request)
			if scenario == "foreign-project" || scenario == "selected-key" || scenario == "null-frame" {
				if err == nil || result.Summary != "" {
					t.Fatal("invalid/secret event projected diagnostics", err)
				}
				return
			}
			var projected struct {
				Exceptions []struct {
					Type, Value string
					Frames      []map[string]any
				}
				Truncated bool
				Untrusted bool `json:"untrusted_content"`
			}
			if err != nil || json.Unmarshal([]byte(sentryDiagnosticFixtureText(t, result.Summary)), &projected) != nil || !projected.Untrusted || strings.Contains(result.Summary, "DROP_") || !utf8.ValidString(result.Summary) || len(result.Summary) > 65536 {
				t.Fatal("bounded event diagnostics failed", err)
			}
			if scenario == "truncated" {
				count := 0
				for _, exception := range projected.Exceptions {
					count += len(exception.Frames)
				}
				if !projected.Truncated || len(projected.Exceptions) != 3 || count != 32 || projected.Exceptions[2].Frames[31]["function"] != "frame-39" {
					t.Fatal("event truncation lost the most recent exception/frame boundary")
				}
			} else if len(projected.Exceptions) != 1 || len(projected.Exceptions[0].Frames) != 1 || projected.Truncated {
				t.Fatal("event diagnostic cardinality changed")
			} else if scenario == "normal" && (projected.Exceptions[0].Frames[0]["line"] != float64(42) || projected.Exceptions[0].Frames[0]["in_app"] != true) {
				t.Fatal("native lineNo/inApp mapping changed")
			} else if scenario == "nullable-frame" && len(projected.Exceptions[0].Frames[0]) != 0 {
				t.Fatal("unknown nullable frame fields fabricated coordinates")
			}
		})
	}
}

// TestSentryFixedProjectCountsConsumer checks explicit opt-in, the exact selected project route/window/stat and complete integer bucket semantics; malformed/unavailable data cannot fabricate totals or affect default metadata health.
// TestSentryFixedProjectCountsConsumer проверяет explicit opt-in, точный selected project route/window/stat и complete integer bucket semantics; неверные/недоступные данные не выдумывают totals и не затрагивают default metadata health.
func TestSentryFixedProjectCountsConsumer(t *testing.T) {
	for _, scenario := range []string{"default", "valid", "missing-bucket", "null-count", "fractional-count", "foreign-window", "duplicate-bucket", "unsupported"} {
		t.Run(scenario, func(t *testing.T) {
			adapter := testAdapter(t)
			input := map[string]any{}
			if scenario != "default" {
				input["include_error_counts"] = true
			}
			request := sentryInvocation(t, adapter, "sentry", "sentry.project.read", input)
			statsCalls := 0
			adapter.openAPIHTTPClient = &http.Client{Transport: roundTripFunc(func(wire *http.Request) (*http.Response, error) {
				if !strings.HasSuffix(wire.URL.Path, "/stats/") {
					return sentryWireFixture(t, wire), nil
				}
				statsCalls++
				query := wire.URL.Query()
				start, firstErr := strconv.ParseInt(query.Get("since"), 10, 64)
				until, lastErr := strconv.ParseInt(query.Get("until"), 10, 64)
				if wire.Header.Get("Authorization") != "Bearer synthetic-sentry-token" || wire.Method != http.MethodGet || wire.URL.Path != "/api/0/projects/fixture-org/fixture-project/stats/" || len(query) != 4 || query.Get("stat") != "received" || query.Get("resolution") != "1h" || firstErr != nil || lastErr != nil || start%3600 != 0 || until+1-start != 86400 {
					t.Fatal("fixed summary escaped its exact project/time/stat boundary")
				}
				buckets := make([][]any, 24)
				for index := range buckets {
					buckets[index] = []any{start + int64(index)*3600, index}
				}
				switch scenario {
				case "missing-bucket":
					buckets = buckets[:23]
				case "null-count":
					buckets[0][1] = nil
				case "fractional-count":
					buckets[0][1] = 1.5
				case "foreign-window":
					buckets[0][0] = start - 3600
				case "duplicate-bucket":
					buckets[1][0] = start
				}
				raw, _ := json.Marshal(buckets)
				status := 200
				if scenario == "unsupported" {
					status, raw = 404, []byte(`{}`)
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: wire}, nil
			})}
			result, err := adapter.Execute(t.Context(), request)
			if scenario == "default" {
				if err != nil || statsCalls != 0 || strings.Contains(result.Summary, "received_counts") {
					t.Fatal("default metadata/health enabled aggregation", err)
				}
			} else if scenario == "valid" {
				var output struct {
					Counts struct {
						State, Stat string
						Total       int64
						Buckets     [][2]int64
					} `json:"received_counts"`
				}
				if err != nil || statsCalls != 1 || json.Unmarshal([]byte(sentryDiagnosticFixtureText(t, result.Summary)), &output) != nil || output.Counts.State != "VALIDATED" || output.Counts.Stat != "received" || output.Counts.Total != 276 || len(output.Counts.Buckets) != 24 {
					t.Fatal("complete native hourly counts were not preserved", err)
				}
			} else if err == nil || result.Summary != "" || statsCalls != 1 {
				t.Fatal("unconfirmed hourly summary became a successful count", err)
			}
		})
	}
}

// sentryDiagnosticFixtureText decodes only the existing tool text envelope before checking its typed Sentry projection; malformed envelopes fail the fixture.
// sentryDiagnosticFixtureText разбирает только существующий tool text envelope перед проверкой typed Sentry projection; неверный envelope закрывает fixture.
func sentryDiagnosticFixtureText(t *testing.T, summary string) string {
	t.Helper()
	var envelope struct {
		Text string `json:"text"`
	}
	if json.Unmarshal([]byte(summary), &envelope) != nil || envelope.Text == "" {
		t.Fatal("missing typed Sentry tool text envelope")
	}
	return envelope.Text
}
