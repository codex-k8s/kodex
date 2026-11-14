package integration

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"go.yaml.in/yaml/v3"
)

// lokiRuleGroup contains only bounded native rule fields needed for safe projection and one closed replacement receipt; arbitrary annotations and routing documents are not accepted as inputs.
// lokiRuleGroup содержит только bounded native rule fields для safe projection и одного closed replacement receipt; arbitrary annotations и routing documents не принимаются как inputs.
type lokiRuleGroup struct {
	Name     string     `yaml:"name"`
	Interval string     `yaml:"interval"`
	Rules    []lokiRule `yaml:"rules"`
}

// lokiRule keeps alert/record identity, duration and literal labels; expression is validated internally but excluded from employee-visible read projections.
// lokiRule сохраняет alert/record identity, duration и literal labels; expression проверяется внутри, но не попадает в employee-visible read projection.
type lokiRule struct {
	Alert  string            `yaml:"alert,omitempty"`
	Record string            `yaml:"record,omitempty"`
	Expr   string            `yaml:"expr"`
	For    string            `yaml:"for,omitempty"`
	Labels map[string]string `yaml:"labels,omitempty"`
}

// lokiGroupProjection drops executable expressions and annotations while preserving rule identity and declared timing; unknown/file-owned configurations remain readable without a writable provenance claim.
// lokiGroupProjection отбрасывает executable expressions и annotations, сохраняя identity и declared timing; unknown/file-owned configuration остаётся readable без утверждения writable provenance.
func lokiGroupProjection(group lokiRuleGroup) (any, error) {
	invalid := &SafeError{Code: providerResponseInvalidCode}
	if !integrationpackage.ValidSentrySlug(group.Name) || len(group.Rules) > 100 || group.Rules == nil || len(group.Interval) > 40 {
		return nil, invalid
	}
	if group.Interval != "" {
		interval, err := time.ParseDuration(group.Interval)
		if err != nil || interval <= 0 || interval > 24*time.Hour {
			return nil, invalid
		}
	}
	rules := []map[string]any{}
	for _, rule := range group.Rules {
		if rule.Alert == "" && rule.Record == "" || rule.Alert != "" && rule.Record != "" || len(rule.Alert) > 200 || len(rule.Record) > 200 || len(rule.Expr) > 8192 || len(rule.For) > 40 {
			return nil, invalid
		}
		if rule.For != "" {
			duration, err := time.ParseDuration(rule.For)
			if err != nil || duration < 0 || duration > 24*time.Hour {
				return nil, invalid
			}
		}
		labels := rule.Labels
		if labels == nil {
			labels = map[string]string{}
		}
		if !observabilityLabels(labels) {
			return nil, invalid
		}
		rules = append(rules, map[string]any{"alert": rule.Alert, "record": rule.Record, "for": rule.For, "labels": labels})
	}
	return map[string]any{"name": group.Name, "interval": group.Interval, "rules": rules}, nil
}

// lokiRuler reads exact namespace/group paths and executes a single full replacement or deletion only through the qualified namespace definition; HTTP202 is a stored/queued receipt, not proof of active evaluation.
// lokiRuler читает exact namespace/group paths и выполняет один full replacement либо deletion только через qualified namespace definition; HTTP202 означает stored/queued receipt, не active evaluation.
func (adapter *Adapter) lokiRuler(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string) (any, string, error) {
	namespace := configuration["rule_namespace"]
	if !integrationpackage.ValidSentrySlug(namespace) {
		return nil, "", &SafeError{Code: providerRequestRejectedCode}
	}
	path := "/loki/api/v1/rules/" + namespace
	reference := "loki-ruler:" + configuration["tenant_id"] + ":" + namespace
	groupName, _ := request.Input["group"].(string)
	if request.Operation != "loki.ruler.group.list" && !integrationpackage.ValidSentrySlug(groupName) {
		return nil, "", &SafeError{Code: providerRequestRejectedCode}
	}
	if request.Operation == "loki.ruler.group.list" {
		response, err := adapter.lokiHTTP(ctx, request, capability, configuration, http.MethodGet, path, nil, nil, false)
		if err != nil {
			return nil, "", err
		}
		var namespaces map[string][]lokiRuleGroup
		if decodeLokiYAML(response.Body, &namespaces) != nil || len(namespaces) != 1 || namespaces[namespace] == nil || len(namespaces[namespace]) > 50 {
			return nil, "", &SafeError{Code: providerResponseInvalidCode}
		}
		groups := []any{}
		seen := map[string]bool{}
		for _, group := range namespaces[namespace] {
			if seen[group.Name] {
				return nil, "", &SafeError{Code: providerResponseInvalidCode}
			}
			seen[group.Name] = true
			projection, err := lokiGroupProjection(group)
			if err != nil {
				return nil, "", err
			}
			groups = append(groups, projection)
		}
		return map[string]any{"items": groups}, reference, nil
	}
	if request.Operation == "loki.ruler.group.read" {
		response, err := adapter.lokiHTTP(ctx, request, capability, configuration, http.MethodGet, path+"/"+groupName, nil, nil, false)
		if err != nil {
			return nil, "", err
		}
		var group lokiRuleGroup
		if decodeLokiYAML(response.Body, &group) != nil || group.Name != groupName {
			return nil, "", &SafeError{Code: providerResponseInvalidCode}
		}
		projection, err := lokiGroupProjection(group)
		return projection, reference + ":" + groupName, err
	}
	if request.DefinitionKey != "loki-ruler-namespace" {
		return nil, "", &SafeError{Code: providerCapabilityUnsupportedCode}
	}
	method := http.MethodDelete
	var body []byte
	var expected lokiRuleGroup
	switch request.Operation {
	case "loki.ruler.group.replace":
		method = http.MethodPost
		alert, _ := request.Input["alert"].(string)
		expression, _ := request.Input["query"].(string)
		severity, _ := request.Input["severity"].(string)
		seconds := observabilityStep(request.Input["for_seconds"])
		if !observabilityLabelName(alert) || !integrationpackage.ValidSentrySlug(severity) || seconds < 1 || seconds > 3600 {
			return nil, "", &SafeError{Code: providerRequestRejectedCode}
		}
		if _, err := validateObservabilityLogQL(expression, "metric"); err != nil {
			return nil, "", err
		}
		expected = lokiRuleGroup{Name: groupName, Interval: "1m", Rules: []lokiRule{{Alert: alert, Expr: expression, For: strconv.Itoa(seconds) + "s", Labels: map[string]string{"severity": severity}}}}
		var err error
		body, err = yaml.Marshal(expected)
		if err != nil {
			return nil, "", &SafeError{Code: providerRequestRejectedCode}
		}
	case "loki.ruler.group.delete":
		path += "/" + groupName
	default:
		return nil, "", &SafeError{Code: providerCapabilityUnsupportedCode}
	}
	response, err := adapter.lokiHTTP(ctx, request, capability, configuration, method, path, nil, body, false)
	if err != nil {
		return nil, "", err
	}
	var receipt struct {
		Status string `json:"status"`
	}
	if response.StatusCode != http.StatusAccepted || decodeTypedProviderJSON(response.Body, &receipt) != nil || receipt.Status != "success" {
		return nil, "", &UnknownOutcomeError{stage: "response_validation"}
	}
	status := "deletion_requested"
	if method == http.MethodPost {
		status = "replacement_stored"
		readback, err := adapter.lokiHTTP(ctx, request, capability, configuration, http.MethodGet, path+"/"+groupName, nil, nil, false)
		if err != nil {
			return nil, "", &UnknownOutcomeError{stage: "response_validation"}
		}
		var current lokiRuleGroup
		if decodeLokiYAML(readback.Body, &current) != nil || current.Name != expected.Name || len(current.Rules) != 1 || current.Rules[0].Alert != expected.Rules[0].Alert || current.Rules[0].Record != "" || strings.TrimSpace(current.Rules[0].Expr) != expected.Rules[0].Expr || len(current.Rules[0].Labels) != 1 || current.Rules[0].Labels["severity"] != expected.Rules[0].Labels["severity"] {
			return nil, "", &UnknownOutcomeError{stage: "response_validation"}
		}
		interval, e1 := time.ParseDuration(current.Interval)
		duration, e2 := time.ParseDuration(current.Rules[0].For)
		if e1 != nil || e2 != nil || interval != time.Minute || duration != time.Duration(observabilityStep(request.Input["for_seconds"]))*time.Second {
			return nil, "", &UnknownOutcomeError{stage: "response_validation"}
		}
	}
	result := map[string]string{"group": groupName, "namespace": namespace, "status": status, "evaluation_state": "UNKNOWN"}
	return result, reference + ":" + groupName, nil
}
