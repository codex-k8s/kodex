package integration

import (
	"encoding/json"
	"strings"
)

// observabilityMetric retains typed samples/histograms and bounded labels while dropping provider execution metadata and arbitrary additional fields.
// observabilityMetric сохраняет typed samples/histograms и ограниченные labels, удаляя provider execution metadata и произвольные дополнительные поля.
type observabilityMetric struct {
	Metric     map[string]string   `json:"metric"`
	Value      []json.RawMessage   `json:"value,omitempty"`
	Values     [][]json.RawMessage `json:"values,omitempty"`
	Histogram  []json.RawMessage   `json:"histogram,omitempty"`
	Histograms [][]json.RawMessage `json:"histograms,omitempty"`
}

// observabilitySample checks either a finite-timestamp/string-value sample or the explicit native-histogram count/sum/buckets representation; unknown bucket rules fail closed.
// observabilitySample проверяет finite-timestamp/string-value sample либо явные native-histogram count/sum/buckets; неизвестные bucket rules закрыто отклоняются.
func observabilitySample(sample []json.RawMessage, histogram bool) bool {
	if len(sample) != 2 || !observabilityTimestamp(sample[0]) {
		return false
	}
	if !histogram {
		var value string
		return observabilityDecode(sample[1], &value) == nil && observabilityNumber(value)
	}
	var value struct {
		Count   string              `json:"count"`
		Sum     string              `json:"sum"`
		Buckets [][]json.RawMessage `json:"buckets"`
	}
	if observabilityDecode(sample[1], &value) != nil || !observabilityNumber(value.Count) || !observabilityNumber(value.Sum) || value.Buckets == nil || len(value.Buckets) > 100 {
		return false
	}
	for _, bucket := range value.Buckets {
		if len(bucket) != 4 {
			return false
		}
		var rule int
		if observabilityDecode(bucket[0], &rule) != nil || rule < 0 || rule > 3 {
			return false
		}
		for _, raw := range bucket[1:] {
			var number string
			if observabilityDecode(raw, &number) != nil || !observabilityNumber(number) {
				return false
			}
		}
	}
	// Re-encode only declared histogram fields, discarding provider additions.
	// Повторный encode сохраняет только объявленные histogram fields без provider additions.
	sample[1], _ = json.Marshal(value)
	return true
}

// observabilityMetricProjection validates the exact result shape and global sample budget; full/possibly truncated series pages are explicitly marked without claiming completeness.
// observabilityMetricProjection проверяет точную форму result и общий sample budget; полные/возможно усечённые страницы явно помечаются без заявления полноты.
func observabilityMetricProjection(operation string, raw []byte) (any, error) {
	var data struct {
		ResultType string          `json:"resultType"`
		Result     json.RawMessage `json:"result"`
	}
	invalid := &SafeError{Code: providerResponseInvalidCode}
	if observabilityData(raw, &data) != nil {
		return nil, invalid
	}
	if data.ResultType == "scalar" || data.ResultType == "string" {
		if operation != "query.instant" {
			return nil, invalid
		}
		var pair []json.RawMessage
		if observabilityDecode(data.Result, &pair) != nil || len(pair) != 2 || !observabilityTimestamp(pair[0]) {
			return nil, invalid
		}
		var text string
		if observabilityDecode(pair[1], &text) != nil || len(text) > 4096 || data.ResultType == "scalar" && !observabilityNumber(text) {
			return nil, invalid
		}
		return map[string]any{"result_type": data.ResultType, "result": pair, "limit_reached": false}, nil
	}
	if operation == "query.range" && data.ResultType != "matrix" || operation == "query.instant" && data.ResultType != "matrix" && data.ResultType != "vector" {
		return nil, invalid
	}
	var metrics []observabilityMetric
	if observabilityDecode(data.Result, &metrics) != nil || metrics == nil || len(metrics) > 50 {
		return nil, invalid
	}
	count := 0
	for _, metric := range metrics {
		if !observabilityLabels(metric.Metric) {
			return nil, invalid
		}
		if data.ResultType == "vector" {
			if len(metric.Values) != 0 || len(metric.Histograms) != 0 || (metric.Value == nil) == (metric.Histogram == nil) {
				return nil, invalid
			}
			if metric.Value != nil && !observabilitySample(metric.Value, false) || metric.Histogram != nil && !observabilitySample(metric.Histogram, true) {
				return nil, invalid
			}
			count++
		} else {
			if metric.Value != nil || metric.Histogram != nil || metric.Values == nil && metric.Histograms == nil {
				return nil, invalid
			}
			for _, sample := range metric.Values {
				if !observabilitySample(sample, false) {
					return nil, invalid
				}
				count++
			}
			for _, sample := range metric.Histograms {
				if !observabilitySample(sample, true) {
					return nil, invalid
				}
				count++
			}
		}
		if count > 5000 {
			return nil, invalid
		}
	}
	return map[string]any{"result_type": data.ResultType, "result": metrics, "limit_reached": len(metrics) == 50}, nil
}

// observabilityProjection dispatches finite safe result types; rules/alerts omit expressions, annotations, diagnostic errors, source files and generator URLs.
// observabilityProjection выбирает конечные безопасные result types; rules/alerts не содержат expressions, annotations, diagnostics, source files и generator URLs.
func observabilityProjection(operation string, raw []byte) (any, error) {
	invalid := &SafeError{Code: providerResponseInvalidCode}
	if strings.HasPrefix(operation, "query.") {
		return observabilityMetricProjection(operation, raw)
	}
	if operation == "labels.list" || operation == "label.values" {
		var values []string
		if observabilityData(raw, &values) != nil || values == nil || len(values) > 200 {
			return nil, invalid
		}
		seen := map[string]bool{}
		for _, value := range values {
			if value == "" || len(value) > 1024 || strings.ContainsAny(value, "\r\n\x00") || seen[value] {
				return nil, invalid
			}
			seen[value] = true
		}
		return map[string]any{"items": values}, nil
	}
	if operation == "series.list" {
		var values []map[string]string
		if observabilityData(raw, &values) != nil || values == nil || len(values) > 50 {
			return nil, invalid
		}
		for _, value := range values {
			if !observabilityLabels(value) {
				return nil, invalid
			}
		}
		return map[string]any{"items": values}, nil
	}
	if operation == "alerts.list" {
		var data struct {
			Alerts []observabilityAlert `json:"alerts"`
		}
		if observabilityData(raw, &data) != nil || data.Alerts == nil || len(data.Alerts) > 100 {
			return nil, invalid
		}
		for _, alert := range data.Alerts {
			if !observabilityAlertValid(alert) {
				return nil, invalid
			}
		}
		return map[string]any{"items": data.Alerts}, nil
	}
	if operation == "rules.list" {
		var data struct {
			Groups []struct {
				Name  string              `json:"name"`
				Rules []observabilityRule `json:"rules"`
			} `json:"groups"`
		}
		if observabilityData(raw, &data) != nil || data.Groups == nil || len(data.Groups) > 50 {
			return nil, invalid
		}
		items := []map[string]any{}
		count := 0
		for _, group := range data.Groups {
			if group.Name == "" || len(group.Name) > 256 || group.Rules == nil {
				return nil, invalid
			}
			for _, rule := range group.Rules {
				count++
				if count > 100 || rule.Name == "" || len(rule.Name) > 256 || rule.Type != "alerting" && rule.Type != "recording" || rule.Health != "ok" && rule.Health != "err" && rule.Health != "unknown" || !observabilityLabels(rule.Labels) {
					return nil, invalid
				}
				if rule.Type == "alerting" && rule.State != "inactive" && rule.State != "pending" && rule.State != "firing" {
					return nil, invalid
				}
			}
			items = append(items, map[string]any{"name": group.Name, "rules": group.Rules})
		}
		return map[string]any{"items": items}, nil
	}
	return nil, &SafeError{Code: providerCapabilityUnsupportedCode}
}

// observabilityAlert retains only bounded state/value and redacted labels; activeAt and provider URLs are not needed to authorize or project this read.
// observabilityAlert сохраняет только ограниченные state/value и скрытые sensitive labels; activeAt и provider URLs для этого чтения не нужны.
type observabilityAlert struct {
	Labels map[string]string `json:"labels"`
	State  string            `json:"state"`
	Value  string            `json:"value"`
}

// observabilityRule is the common read-only rule projection; no source file or reusable configuration is returned.
// observabilityRule задаёт общую read-only проекцию rule без source file и многоразовой конфигурации.
type observabilityRule struct {
	Name   string            `json:"name"`
	Type   string            `json:"type"`
	Health string            `json:"health"`
	State  string            `json:"state,omitempty"`
	Labels map[string]string `json:"labels"`
}

// observabilityAlertValid rejects unknown alert states and malformed metric values before projecting the provider object.
// observabilityAlertValid отклоняет неизвестные alert states и повреждённые metric values до проекции provider object.
func observabilityAlertValid(alert observabilityAlert) bool {
	return observabilityLabels(alert.Labels) && (alert.State == "pending" || alert.State == "firing") && observabilityNumber(alert.Value)
}
