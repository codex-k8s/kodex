package integration

import (
	"context"
	"net/http"
	"strconv"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// grafanaDashboardIdentity is a safe dashboard projection; targets, variables, links, plugin configuration and credentials are intentionally absent.
// grafanaDashboardIdentity задаёт safe dashboard projection без targets, variables, links, plugin configuration и credentials.
type grafanaDashboardIdentity struct {
	UID     string `json:"uid"`
	Title   string `json:"title"`
	Version int64  `json:"version"`
}

// grafanaDashboard resolves a literal UID through the currently authenticated organization and projects metadata only; it grants no dashboard mutation authority.
// grafanaDashboard разрешает literal UID внутри current authenticated organization и выдаёт только metadata; dashboard mutation authority отсутствует.
func (adapter *Adapter) grafanaDashboard(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, uid string) (grafanaDashboardIdentity, error) {
	if !integrationpackage.ValidSentrySlug(uid) {
		return grafanaDashboardIdentity{}, &SafeError{Code: providerRequestRejectedCode}
	}
	response, err := adapter.grafanaJSON(ctx, request, capability, configuration, http.MethodGet, "/api/dashboards/uid/"+uid, nil, nil, false)
	if err != nil {
		return grafanaDashboardIdentity{}, err
	}
	var data struct {
		Dashboard grafanaDashboardIdentity `json:"dashboard"`
	}
	if grafanaDecode(response.Body, &data) != nil || data.Dashboard.UID != uid || data.Dashboard.Title == "" || len(data.Dashboard.Title) > 1000 || data.Dashboard.Version < 1 {
		return grafanaDashboardIdentity{}, &SafeError{Code: providerResponseInvalidCode}
	}
	return data.Dashboard, nil
}

// grafanaDatasourceIdentity excludes URL, database/user, headers, JSON settings and secure JSON metadata from the selected provider object.
// grafanaDatasourceIdentity исключает URL, database/user, headers, JSON settings и secure JSON metadata выбранного provider object.
type grafanaDatasourceIdentity struct {
	UID   string `json:"uid"`
	OrgID int64  `json:"orgId"`
	Name  string `json:"name"`
	Type  string `json:"type"`
}

// grafanaDatasource checks current organization, UID and supported provider type before projecting only identity; this GET is not treated as an atomic plugin-type lock.
// grafanaDatasource проверяет current organization, UID и supported provider type до identity projection; этот GET не считается atomic plugin-type lock.
func (adapter *Adapter) grafanaDatasource(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string) (grafanaDatasourceIdentity, error) {
	response, err := adapter.grafanaJSON(ctx, request, capability, configuration, http.MethodGet, "/api/datasources/uid/"+configuration["datasource_uid"], nil, nil, false)
	if err != nil {
		return grafanaDatasourceIdentity{}, err
	}
	var source grafanaDatasourceIdentity
	if grafanaDecode(response.Body, &source) != nil || source.UID != configuration["datasource_uid"] || strconv.FormatInt(source.OrgID, 10) != configuration["organization_id"] || source.Type != "prometheus" || source.Name == "" || len(source.Name) > 500 {
		return grafanaDatasourceIdentity{}, &SafeError{Code: providerResponseInvalidCode}
	}
	return source, nil
}

// grafanaDatasourceQuery uses only a fixed GET Prometheus route under one selected Grafana datasource UID, avoiding generic plugin POSTs or caller-controlled proxy paths and retaining the shared PromQL AST/result budgets.
// grafanaDatasourceQuery использует только fixed GET Prometheus route одного выбранного Grafana datasource UID без generic plugin POST и caller-controlled proxy paths, сохраняя общие PromQL AST/result budgets.
func (adapter *Adapter) grafanaDatasourceQuery(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string) (any, error) {
	if _, err := adapter.grafanaDatasource(ctx, request, capability, configuration); err != nil {
		return nil, err
	}
	path, query, err := observabilityQuery("query.range", request.Input)
	if err != nil {
		return nil, err
	}
	query.Set("partial_response", "false")
	query.Set("dedup", "true")
	response, err := adapter.grafanaJSON(ctx, request, capability, configuration, http.MethodGet, "/api/datasources/proxy/uid/"+configuration["datasource_uid"]+path, query, nil, false)
	if err != nil {
		return nil, err
	}
	return observabilityMetricProjection("query.range", response.Body)
}

// grafanaRuleReadProjection exposes only rule identity/state/provenance; query models, annotations, routing configuration and contact-point secrets never leave the consumer.
// grafanaRuleReadProjection выдаёт только rule identity/state/provenance; query models, annotations, routing configuration и contact-point secrets остаются внутри consumer.
type grafanaRuleReadProjection struct {
	UID        string `json:"uid"`
	OrgID      int64  `json:"orgID"`
	Title      string `json:"title"`
	FolderUID  string `json:"folderUID"`
	RuleGroup  string `json:"ruleGroup"`
	Provenance string `json:"provenance"`
	Paused     *bool  `json:"isPaused"`
}

// grafanaRuleValid checks currently organization-bound identity and bounded display fields without assigning writable provenance to unknown or file/Git-managed rules.
// grafanaRuleValid проверяет current organization-bound identity и bounded display fields без назначения writable provenance unknown либо file/Git-managed rules.
func grafanaRuleValid(rule grafanaRuleReadProjection, organization, expected string) bool {
	return integrationpackage.ValidSentrySlug(rule.UID) && (expected == "" || rule.UID == expected) && strconv.FormatInt(rule.OrgID, 10) == organization && integrationpackage.ValidSentrySlug(rule.FolderUID) && rule.Title != "" && len(rule.Title) <= 1000 && rule.RuleGroup != "" && len(rule.RuleGroup) <= 200 && len(rule.Provenance) <= 100 && rule.Paused != nil
}

// grafanaRulesRead supports bounded current organization list/detail without exporting reusable rule configuration or interpreting provenance as an atomic write precondition.
// grafanaRulesRead поддерживает bounded current organization list/detail без выдачи reusable rule configuration и без трактовки provenance как atomic write precondition.
func (adapter *Adapter) grafanaRulesRead(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string) (any, string, error) {
	path := "/api/v1/provisioning/alert-rules"
	uid, _ := request.Input["rule_uid"].(string)
	if request.Operation == "grafana.rule.read" {
		if !integrationpackage.ValidSentrySlug(uid) {
			return nil, "", &SafeError{Code: providerRequestRejectedCode}
		}
		path += "/" + uid
	} else if request.Operation != "grafana.rule.list" {
		return nil, "", &SafeError{Code: providerCapabilityUnsupportedCode}
	}
	response, err := adapter.grafanaJSON(ctx, request, capability, configuration, http.MethodGet, path, nil, nil, false)
	if err != nil {
		return nil, "", err
	}
	rules := []grafanaRuleReadProjection{}
	if uid != "" {
		var rule grafanaRuleReadProjection
		if grafanaDecode(response.Body, &rule) != nil {
			return nil, "", &SafeError{Code: providerResponseInvalidCode}
		}
		rules = append(rules, rule)
	} else if grafanaDecode(response.Body, &rules) != nil || rules == nil || len(rules) > 50 {
		return nil, "", &SafeError{Code: providerResponseInvalidCode}
	}
	seen := map[string]bool{}
	for _, rule := range rules {
		if !grafanaRuleValid(rule, configuration["organization_id"], uid) || seen[rule.UID] {
			return nil, "", &SafeError{Code: providerResponseInvalidCode}
		}
		seen[rule.UID] = true
	}
	return map[string]any{"items": rules, "limit_reached": len(rules) == 50}, "grafana-rules:" + configuration["organization_id"], nil
}

// grafanaAnnotation is the safe current annotation projection; author identity, email, avatar, embedded data and links are not decoded into output.
// grafanaAnnotation задаёт safe current annotation projection без author identity, email, avatar, embedded data и links.
type grafanaAnnotation struct {
	ID           int64   `json:"id"`
	DashboardUID *string `json:"dashboardUID,omitempty"`
	Time         int64   `json:"time"`
	TimeEnd      int64   `json:"timeEnd"`
	Text         string  `json:"text"`
}

// grafanaAnnotationValid preserves the exact dashboard READ boundary and bounded fields; organization-scoped writes allow provider reassociation within the already granted organization.
// grafanaAnnotationValid сохраняет exact dashboard READ boundary и bounded fields; organization-scoped writes допускают provider reassociation внутри уже granted organization.
func grafanaAnnotationValid(item grafanaAnnotation, dashboard string, id int64) bool {
	return item.ID > 0 && (id == 0 || item.ID == id) && item.Time > 0 && item.TimeEnd >= item.Time && item.Text != "" && len(item.Text) <= 2000 && (dashboard == "" || item.DashboardUID != nil && *item.DashboardUID == dashboard) && (item.DashboardUID == nil || *item.DashboardUID == "" || integrationpackage.ValidSentrySlug(*item.DashboardUID))
}

// grafanaAnnotationRead resolves current provider ownership before any detail projection/effect, never using a caller ID as evidence of dashboard membership.
// grafanaAnnotationRead разрешает current provider ownership перед detail projection/effect, не считая caller ID доказательством dashboard membership.
func (adapter *Adapter) grafanaAnnotationRead(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, id int64) (grafanaAnnotation, error) {
	response, err := adapter.grafanaJSON(ctx, request, capability, configuration, http.MethodGet, "/api/annotations/"+strconv.FormatInt(id, 10), nil, nil, false)
	if err != nil {
		return grafanaAnnotation{}, err
	}
	var item grafanaAnnotation
	dashboard := ""
	if request.DefinitionKey == "grafana-dashboard" {
		dashboard = configuration["dashboard_uid"]
	}
	if grafanaDecode(response.Body, &item) != nil || !grafanaAnnotationValid(item, dashboard, id) {
		return grafanaAnnotation{}, &SafeError{Code: providerResponseInvalidCode}
	}
	return item, nil
}

// grafanaIntegerID accepts only positive canonical signed-64-bit provider identifiers without URL syntax, fractional numbers or overflow.
// grafanaIntegerID допускает только положительные canonical signed-64-bit provider IDs без URL syntax, fractional numbers и overflow.
func grafanaIntegerID(value string) int64 {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != value {
		return 0
	}
	return id
}

// grafanaAnnotationScope checks only current provider permission coordinates; the exact organization-wide grant remains separately mandatory.
// grafanaAnnotationScope проверяет только current provider permission coordinates; exact organization-wide grant остаётся отдельно обязательным.
func grafanaAnnotationScope(permissions map[string][]string, operation string, item grafanaAnnotation) bool {
	targets := []string{"annotations:id:" + strconv.FormatInt(item.ID, 10)}
	if item.DashboardUID == nil || *item.DashboardUID == "" {
		targets = append(targets, "annotations:type:organization")
	} else {
		targets = append(targets, "dashboards:uid:"+*item.DashboardUID)
	}
	return grafanaPermissionScope(permissions, grafanaWriteAction(operation), targets...)
}
