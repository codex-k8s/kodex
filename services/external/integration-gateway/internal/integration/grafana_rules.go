package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// grafanaWritableRule retains only current identity and the finite configuration emitted by this consumer; provider secrets and unrecognized settings cannot reach output.
// grafanaWritableRule сохраняет только текущую identity и конечную конфигурацию consumer; provider secrets и неизвестные настройки не попадают в output.
type grafanaWritableRule struct {
	UID          string             `json:"uid"`
	OrgID        int64              `json:"orgID"`
	Title        string             `json:"title"`
	FolderUID    string             `json:"folderUID"`
	RuleGroup    string             `json:"ruleGroup"`
	Provenance   string             `json:"provenance"`
	Paused       *bool              `json:"isPaused"`
	Condition    string             `json:"condition"`
	Data         []grafanaRuleQuery `json:"data"`
	NoDataState  string             `json:"noDataState"`
	ExecErrState string             `json:"execErrState"`
	For          string             `json:"for"`
}

// grafanaRuleQuery limits readback to two server-built query models rather than accepting a client-controlled datasource/plugin document.
// grafanaRuleQuery ограничивает readback двумя server-built query models вместо client-controlled datasource/plugin document.
type grafanaRuleQuery struct {
	RefID             string           `json:"refId"`
	DatasourceUID     string           `json:"datasourceUid"`
	RelativeTimeRange map[string]int64 `json:"relativeTimeRange"`
	Model             map[string]any   `json:"model"`
}

// grafanaReadWritableRule resolves the UID inside the current organization and refuses file/Git/unknown provenance before a mutation; the native write still supplies its own atomic manager/version boundary.
// grafanaReadWritableRule разрешает UID внутри текущей организации и отвергает file/Git/unknown provenance до mutation; atomic manager/version boundary обеспечивает и native write.
func (adapter *Adapter) grafanaReadWritableRule(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, uid string) (grafanaWritableRule, error) {
	if !integrationpackage.ValidSentrySlug(uid) || len(uid) > 40 {
		return grafanaWritableRule{}, &SafeError{Code: providerRequestRejectedCode}
	}
	response, err := adapter.grafanaJSON(ctx, request, capability, configuration, http.MethodGet, "/api/v1/provisioning/alert-rules/"+uid, nil, nil, false)
	if err != nil {
		return grafanaWritableRule{}, err
	}
	var rule grafanaWritableRule
	if decodeTypedProviderJSON(response.Body, &rule) != nil || !grafanaRuleValid(rule.projection(), configuration["organization_id"], uid) {
		return grafanaWritableRule{}, &SafeError{Code: providerResponseInvalidCode}
	}
	if rule.Provenance != "api" {
		return grafanaWritableRule{}, &SafeError{Code: providerCapabilityUnsupportedCode}
	}
	return rule, nil
}

// grafanaRulePayload constructs one fully replaced threshold rule from finite scalar fields, with selected datasource and organization assigned by the server and a deterministic create UID bound to the approved effect.
// grafanaRulePayload строит целиком заменяемое threshold rule из конечных scalar fields; выбранные datasource/organization назначает сервер, а create UID связан с approved effect.
func grafanaRulePayload(request Request, configuration map[string]string) (map[string]any, error) {
	title, _ := request.Input["title"].(string)
	folder, _ := request.Input["folder_uid"].(string)
	group, _ := request.Input["rule_group"].(string)
	query, _ := request.Input["query"].(string)
	comparator, _ := request.Input["comparator"].(string)
	thresholdText, _ := request.Input["threshold"].(string)
	paused, ok := request.Input["paused"].(bool)
	threshold, err := strconv.ParseFloat(thresholdText, 64)
	seconds := observabilityStep(request.Input["for_seconds"])
	if err != nil || !ok || title == "" || len(title) > 190 || !integrationpackage.ValidSentrySlug(folder) || len(folder) > 40 || group == "" || len(group) > 190 || len(thresholdText) > 32 || math.IsNaN(threshold) || math.IsInf(threshold, 0) || math.Abs(threshold) > 1e9 || (comparator != "gt" && comparator != "lt") || seconds < 1 || seconds > 3600 || validateObservabilityPromQL(query, false) != nil {
		return nil, &SafeError{Code: providerRequestRejectedCode}
	}
	uid, _ := request.Input["rule_uid"].(string)
	if request.Operation == "grafana.rule.create" {
		hash := sha256.Sum256([]byte(request.EffectKey + "\x00" + request.ResourceScopeDigest))
		uid = "kodex-" + hex.EncodeToString(hash[:16])
	}
	if !integrationpackage.ValidSentrySlug(uid) || len(uid) > 40 {
		return nil, &SafeError{Code: providerRequestRejectedCode}
	}
	org, _ := strconv.ParseInt(configuration["organization_id"], 10, 64)
	data := []grafanaRuleQuery{
		{RefID: "A", DatasourceUID: configuration["datasource_uid"], RelativeTimeRange: map[string]int64{"from": 300, "to": 0}, Model: map[string]any{"refId": "A", "expr": query, "instant": true, "range": false, "intervalMs": 1000, "maxDataPoints": 1000}},
		{RefID: "B", DatasourceUID: "__expr__", RelativeTimeRange: map[string]int64{"from": 0, "to": 0}, Model: map[string]any{"refId": "B", "type": "classic_conditions", "conditions": []any{map[string]any{"type": "query", "query": map[string]any{"params": []string{"A"}}, "reducer": map[string]any{"type": "last", "params": []any{}}, "operator": map[string]any{"type": "and"}, "evaluator": map[string]any{"type": comparator, "params": []float64{threshold}}}}}},
	}
	return map[string]any{"uid": uid, "orgID": org, "folderUID": folder, "ruleGroup": group, "title": title, "condition": "B", "data": data, "noDataState": "NoData", "execErrState": "Error", "for": strconv.Itoa(seconds) + "s", "isPaused": paused}, nil
}

// grafanaRuleReceipt checks current identity plus the server-built replacement model after effect; unexpected normalization or a failed readback remains UNKNOWN rather than pretending the replacement was confirmed.
// grafanaRuleReceipt проверяет текущую identity и server-built replacement model после effect; неожиданная normalization либо failed readback остаётся UNKNOWN вместо ложного подтверждения replacement.
func grafanaRuleReceipt(rule grafanaWritableRule, wanted map[string]any) bool {
	encoded, _ := json.Marshal(wanted)
	var expected grafanaWritableRule
	if decodeTypedProviderJSON(encoded, &expected) != nil {
		return false
	}
	if rule.UID != expected.UID || rule.OrgID != expected.OrgID || rule.FolderUID != expected.FolderUID || rule.Title != expected.Title || rule.RuleGroup != expected.RuleGroup || rule.Condition != "B" || rule.NoDataState != "NoData" || rule.ExecErrState != "Error" || rule.Paused == nil || expected.Paused == nil || *rule.Paused != *expected.Paused || len(rule.Data) != 2 {
		return false
	}
	duration, err := time.ParseDuration(rule.For)
	wantedDuration, _ := time.ParseDuration(expected.For)
	if err != nil || duration != wantedDuration {
		return false
	}
	for i, item := range expected.Data {
		actual := rule.Data[i]
		if actual.RefID != item.RefID || actual.DatasourceUID != item.DatasourceUID || actual.RelativeTimeRange["from"] != item.RelativeTimeRange["from"] || actual.RelativeTimeRange["to"] != item.RelativeTimeRange["to"] {
			return false
		}
		for key, value := range item.Model {
			a, _ := json.Marshal(actual.Model[key])
			b, _ := json.Marshal(value)
			if string(a) != string(b) {
				return false
			}
		}
	}
	return true
}

// grafanaRuleMutation performs one EACH-gated fixed create/update/delete; native manager/version checks protect C/U while DELETE additionally requires the separately selected API-only organization qualification.
// grafanaRuleMutation выполняет один EACH-gated fixed create/update/delete; native manager/version checks защищают C/U, а DELETE требует отдельно выбранной API-only organization qualification.
func (adapter *Adapter) grafanaRuleMutation(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, permissions map[string][]string) (any, string, error) {
	uid, _ := request.Input["rule_uid"].(string)
	path := "/api/v1/provisioning/alert-rules"
	var current grafanaWritableRule
	var err error
	if request.Operation != "grafana.rule.create" {
		current, err = adapter.grafanaReadWritableRule(ctx, request, capability, configuration, uid)
		if err != nil {
			return nil, "", err
		}
		if !grafanaPermissionScope(permissions, grafanaWriteAction(request.Operation), "folders:uid:"+current.FolderUID) {
			return nil, "", &SafeError{Code: providerRequestRejectedCode}
		}
		path += "/" + uid
	}
	if request.Operation == "grafana.rule.delete" {
		response, err := adapter.grafanaJSON(ctx, request, capability, configuration, http.MethodDelete, path, nil, nil, false)
		if err != nil {
			return nil, "", err
		}
		if response.StatusCode != http.StatusNoContent {
			return nil, "", &UnknownOutcomeError{stage: "response_validation"}
		}
		return map[string]string{"uid": uid, "status": "deletion_requested"}, "grafana-rule:" + configuration["organization_id"] + ":" + uid, nil
	}
	if request.Operation != "grafana.rule.create" && request.Operation != "grafana.rule.update" {
		return nil, "", &SafeError{Code: providerCapabilityUnsupportedCode}
	}
	body, err := grafanaRulePayload(request, configuration)
	if err != nil {
		return nil, "", err
	}
	if !grafanaPermissionScope(permissions, grafanaWriteAction(request.Operation), "folders:uid:"+body["folderUID"].(string)) {
		return nil, "", &SafeError{Code: providerRequestRejectedCode}
	}
	if _, err = adapter.grafanaDatasource(ctx, request, capability, configuration); err != nil {
		return nil, "", err
	}
	method := http.MethodPost
	if request.Operation == "grafana.rule.update" {
		method = http.MethodPut
	}
	response, err := adapter.grafanaJSON(ctx, request, capability, configuration, method, path, nil, body, false)
	if err != nil {
		return nil, "", err
	}
	var receipt grafanaWritableRule
	if decodeTypedProviderJSON(response.Body, &receipt) != nil || !grafanaRuleReceipt(receipt, body) {
		return nil, "", &UnknownOutcomeError{stage: "response_validation"}
	}
	uid = body["uid"].(string)
	confirmed, err := adapter.grafanaReadWritableRule(ctx, request, capability, configuration, uid)
	if err != nil || !grafanaRuleReceipt(confirmed, body) {
		return nil, "", &UnknownOutcomeError{stage: "response_validation"}
	}
	return confirmed.projection(), "grafana-rule:" + configuration["organization_id"] + ":" + uid, nil
}

// projection discards query models and internal receipt configuration from employee-visible rule results.
// projection отбрасывает query models и внутреннюю receipt configuration из employee-visible rule results.
func (rule grafanaWritableRule) projection() grafanaRuleReadProjection {
	return grafanaRuleReadProjection{UID: rule.UID, OrgID: rule.OrgID, Title: rule.Title, FolderUID: rule.FolderUID, RuleGroup: rule.RuleGroup, Provenance: rule.Provenance, Paused: rule.Paused}
}
