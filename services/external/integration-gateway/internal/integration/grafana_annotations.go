package integration

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// grafanaAnnotations consumes fixed annotation commands, keeping legacy-ID mutations organization-scoped until a qualified immutable narrow storage profile is selected.
// grafanaAnnotations исполняет fixed annotation commands, сохраняя organization scope legacy-ID mutations до выбора qualified immutable narrow storage profile.
func (adapter *Adapter) grafanaAnnotations(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, permissions map[string][]string) (any, string, error) {
	reference := "grafana-annotations:" + configuration["organization_id"]
	if request.Operation == "grafana.annotation.list" {
		start, err := observabilityTime(request.Input, "start")
		if err != nil {
			return nil, "", err
		}
		end, err := observabilityTime(request.Input, "end")
		if err != nil {
			return nil, "", err
		}
		if end.Before(start) || end.Sub(start).Hours() > 24 {
			return nil, "", &SafeError{Code: providerRequestRejectedCode}
		}
		query := url.Values{"from": {strconv.FormatInt(start.UnixMilli(), 10)}, "to": {strconv.FormatInt(end.UnixMilli(), 10)}, "limit": {"50"}}
		dashboard := ""
		if request.DefinitionKey == "grafana-dashboard" || request.DefinitionKey == "grafana-dashboard-annotations" {
			dashboard = configuration["dashboard_uid"]
			query.Set("dashboardUID", dashboard)
		}
		response, err := adapter.grafanaJSON(ctx, request, capability, configuration, http.MethodGet, "/api/annotations", query, nil, false)
		if err != nil {
			return nil, "", err
		}
		var items []grafanaAnnotation
		if decodeTypedProviderJSON(response.Body, &items) != nil || items == nil || len(items) > 50 {
			return nil, "", &SafeError{Code: providerResponseInvalidCode}
		}
		seen := map[int64]bool{}
		for _, item := range items {
			if !grafanaAnnotationValid(item, dashboard, 0) || seen[item.ID] {
				return nil, "", &SafeError{Code: providerResponseInvalidCode}
			}
			seen[item.ID] = true
		}
		return map[string]any{"items": items, "limit_reached": len(items) == 50}, reference, nil
	}
	idText, _ := request.Input["annotation_id"].(string)
	id := grafanaIntegerID(idText)
	var item grafanaAnnotation
	var err error
	if request.Operation != "grafana.annotation.create" {
		if id == 0 {
			return nil, "", &SafeError{Code: providerRequestRejectedCode}
		}
		item, err = adapter.grafanaAnnotationRead(ctx, request, capability, configuration, id)
		if err != nil {
			return nil, "", err
		}
		if request.Operation == "grafana.annotation.read" {
			return item, reference + ":" + idText, nil
		}
	}
	if request.Operation == "grafana.annotation.create" {
		dashboard, _ := request.Input["dashboard_uid"].(string)
		if request.DefinitionKey == "grafana-dashboard-annotations" {
			dashboard = configuration["dashboard_uid"]
		}
		if _, err := adapter.grafanaDashboard(ctx, request, capability, configuration, dashboard); err != nil {
			return nil, "", err
		}
		item.DashboardUID = &dashboard
	}
	if !grafanaAnnotationScope(permissions, request.Operation, item) {
		return nil, "", &SafeError{Code: providerRequestRejectedCode}
	}
	if request.Operation == "grafana.annotation.delete" {
		response, err := adapter.grafanaJSON(ctx, request, capability, configuration, http.MethodDelete, "/api/annotations/"+idText, nil, nil, false)
		if err != nil {
			return nil, "", err
		}
		var receipt struct {
			Message string `json:"message"`
		}
		if decodeTypedProviderJSON(response.Body, &receipt) != nil || receipt.Message != "Annotation deleted" {
			return nil, "", &UnknownOutcomeError{stage: "response_validation"}
		}
		return map[string]string{"id": idText, "status": "deletion_requested"}, reference + ":" + idText, nil
	}
	if request.Operation != "grafana.annotation.create" && request.Operation != "grafana.annotation.update" {
		return nil, "", &SafeError{Code: providerCapabilityUnsupportedCode}
	}
	start, err := observabilityTime(request.Input, "time")
	if err != nil {
		return nil, "", err
	}
	end, err := observabilityTime(request.Input, "time_end")
	if err != nil {
		return nil, "", err
	}
	text, _ := request.Input["text"].(string)
	if text == "" || len(text) > 2000 || end.Before(start) || end.Sub(start).Hours() > 24 || start.Nanosecond()%1000000 != 0 || end.Nanosecond()%1000000 != 0 {
		return nil, "", &SafeError{Code: providerRequestRejectedCode}
	}
	body := map[string]any{"text": text, "time": start.UnixMilli(), "timeEnd": end.UnixMilli()}
	path, method := "/api/annotations/"+idText, http.MethodPatch
	if request.Operation == "grafana.annotation.create" {
		method, path = http.MethodPost, "/api/annotations"
		body["dashboardUID"] = *item.DashboardUID
	}
	response, err := adapter.grafanaJSON(ctx, request, capability, configuration, method, path, nil, body, false)
	if err != nil {
		return nil, "", err
	}
	var receipt struct {
		ID      int64  `json:"id"`
		Message string `json:"message"`
	}
	if decodeTypedProviderJSON(response.Body, &receipt) != nil {
		return nil, "", &UnknownOutcomeError{stage: "response_validation"}
	}
	if method == http.MethodPost {
		if receipt.ID < 1 || receipt.Message != "Annotation added" {
			return nil, "", &UnknownOutcomeError{stage: "response_validation"}
		}
		id = receipt.ID
		idText = strconv.FormatInt(id, 10)
	} else if receipt.Message != "Annotation patched" {
		return nil, "", &UnknownOutcomeError{stage: "response_validation"}
	}
	confirmed, err := adapter.grafanaAnnotationRead(ctx, request, capability, configuration, id)
	if err != nil || confirmed.Text != text || confirmed.Time != start.UnixMilli() || confirmed.TimeEnd != end.UnixMilli() || method == http.MethodPost && (confirmed.DashboardUID == nil || *confirmed.DashboardUID != *item.DashboardUID) {
		return nil, "", &UnknownOutcomeError{stage: "response_validation"}
	}
	return confirmed, reference + ":" + idText, nil
}
