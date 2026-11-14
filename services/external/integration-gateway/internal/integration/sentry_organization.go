package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// sentryOrganization is current provider identity for an explicitly organization-wide connection; no project connection can manufacture this authority.
// sentryOrganization хранит текущую provider identity явно organization-wide подключения; project connection не создаёт эти полномочия.
type sentryOrganization struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// sentryDetector projects monitor identity and its server-owned project coordinate without copying arbitrary provider configuration or data sources.
// sentryDetector проецирует monitor identity и server-owned project coordinate без произвольной конфигурации или data sources провайдера.
type sentryDetector struct {
	ID        string  `json:"id"`
	ProjectID *string `json:"projectId"`
	Name      string  `json:"name"`
	Type      string  `json:"type"`
}

// sentryWorkflow pins the closed condition/email-action profile used for deterministic create/update readback; unrelated provider configuration is not accepted from input.
// sentryWorkflow закрепляет закрытый condition/email-action profile для deterministic create/update readback; посторонняя конфигурация не принимается из input.
type sentryWorkflow struct {
	sentryAlert
	DetectorIDs []string `json:"detectorIds"`
	Config      struct {
		Frequency *int `json:"frequency"`
	} `json:"config"`
	Triggers struct {
		LogicType  string `json:"logicType"`
		Conditions []struct {
			Type       string `json:"type"`
			Comparison *bool  `json:"comparison"`
			Result     *bool  `json:"conditionResult"`
		} `json:"conditions"`
	} `json:"triggers"`
	ActionFilters []struct {
		LogicType  string            `json:"logicType"`
		Conditions []json.RawMessage `json:"conditions"`
		Actions    []struct {
			Type string `json:"type"`
			Data struct {
				Fallthrough string `json:"fallthrough_type"`
			} `json:"data"`
			Config struct {
				TargetType string `json:"target_type"`
				TargetID   string `json:"target_identifier"`
			} `json:"config"`
		} `json:"actions"`
	} `json:"actionFilters"`
}

// sentryReadOrganization validates the exact organization slug and canonical ID via the same selected provider transport used for all later operations.
// sentryReadOrganization проверяет точный organization slug и canonical ID через тот же provider transport, что последующие операции.
func (adapter *Adapter) sentryReadOrganization(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string) (sentryOrganization, error) {
	response, err := adapter.sentryJSON(ctx, request, capability, configuration, http.MethodGet, "/api/0/organizations/"+configuration["organization_slug"]+"/", nil, nil)
	if err != nil {
		return sentryOrganization{}, err
	}
	var organization sentryOrganization
	if integrationpackage.ValidateCredentialPermissionJSON(response.Body) != nil || decodeProviderJSON(response.Body, &organization) != nil || !sentryDecimal(organization.ID) || organization.Slug != configuration["organization_slug"] || organization.Name == "" || len(organization.Name) > 1000 {
		return sentryOrganization{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return organization, nil
}

// sentryWorkflowScope derives the complete current affected-project set for organization-authorized operations; an all-project marker remains explicit and never becomes a project-only proof.
// sentryWorkflowScope выводит полную текущую область затронутых проектов organization-authorized операций; all-project marker остаётся явным и не становится project-only proof.
func (adapter *Adapter) sentryWorkflowScope(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, id string) (map[string]any, error) {
	response, err := adapter.sentryJSON(ctx, request, capability, configuration, http.MethodGet, "/api/0/organizations/"+configuration["organization_slug"]+"/workflows/"+id+"/project-scope/", nil, nil)
	if err != nil {
		return nil, err
	}
	var scope struct {
		ProjectIDs *[]string `json:"projectIds"`
		All        *bool     `json:"includesAllProjects"`
	}
	if integrationpackage.ValidateCredentialPermissionJSON(response.Body) != nil || decodeProviderJSON(response.Body, &scope) != nil || scope.ProjectIDs == nil || scope.All == nil || len(*scope.ProjectIDs) > 100 || *scope.All && len(*scope.ProjectIDs) != 0 {
		return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	seen := map[string]bool{}
	for _, id := range *scope.ProjectIDs {
		if !sentryDecimal(id) || seen[id] {
			return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		seen[id] = true
	}
	return map[string]any{"project_ids": *scope.ProjectIDs, "includes_all_projects": *scope.All}, nil
}

// sentryOrganizationReleaseProjection validates a complete bounded release membership list and emits only safe metadata under the explicit full-organization grant.
// sentryOrganizationReleaseProjection проверяет полный ограниченный состав проектов release и выдаёт безопасные metadata только под явным full-organization grant.
func sentryOrganizationReleaseProjection(release sentryRelease, version string) (map[string]any, error) {
	if !sentryVersion(release.Version) || version != "" && release.Version != version || len(release.Projects) < 1 || len(release.Projects) > 100 || release.Status != "open" && release.Status != "archived" || release.Ref != nil && len(*release.Ref) > 500 {
		return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	projects := []map[string]string{}
	seen := map[string]bool{}
	for _, member := range release.Projects {
		id := string(member.ID)
		if !sentryDecimal(id) || !integrationpackage.ValidSentrySlug(member.Slug) || seen[id] {
			return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		seen[id] = true
		projects = append(projects, map[string]string{"id": id, "slug": member.Slug})
	}
	result := map[string]any{"version": release.Version, "status": release.Status, "affected_projects": projects}
	if release.Ref != nil {
		result["ref"] = *release.Ref
	}
	return result, nil
}

// sentryNotificationTarget accepts only a Sentry user/team numeric reference or the explicit issue-owner recipient; arbitrary addresses, webhooks and integration credentials are excluded.
// sentryNotificationTarget допускает только numeric reference пользователя/команды Sentry либо явного получателя issue owners; произвольные адреса, webhooks и integration credentials исключены.
func sentryNotificationTarget(value string) (string, string, bool) {
	if value == "issue_owners" {
		return value, "", true
	}
	kind, id, found := strings.Cut(value, ":")
	return kind, id, found && (kind == "user" || kind == "team") && sentryDecimal(id)
}

// sentryWorkflowPayload maps finite scalar inputs to one email-only workflow configuration and one provider monitor, rather than forwarding a generic config document.
// sentryWorkflowPayload сопоставляет конечные scalar inputs одной email-only конфигурации workflow и одному provider monitor вместо передачи универсального config document.
func sentryWorkflowPayload(input map[string]any) (map[string]any, error) {
	detector, _ := input["detector_id"].(string)
	name, _ := input["name"].(string)
	enabled, enabledPresent := input["enabled"].(bool)
	trigger, _ := input["trigger"].(string)
	target, _ := input["notification_target"].(string)
	kind, id, valid := sentryNotificationTarget(target)
	frequency := 0
	switch raw := input["frequency_minutes"].(type) {
	case int:
		frequency = raw
	case float64:
		frequency = int(raw)
	case json.Number:
		frequency, _ = strconv.Atoi(string(raw))
	}
	detectorID, parseErr := strconv.ParseInt(detector, 10, 64)
	if !sentryDecimal(detector) || parseErr != nil || name == "" || len(name) > 256 || !enabledPresent || !valid || frequency < 1 || frequency > 10080 || (trigger != "first_seen_event" && trigger != "regression_event" && trigger != "reappeared_event") {
		return nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	config := map[string]any{"target_type": kind}
	if id != "" {
		config["target_identifier"] = id
	}
	return map[string]any{"name": name, "enabled": enabled, "detectorIds": []int64{detectorID}, "config": map[string]int{"frequency": frequency}, "triggers": map[string]any{"logicType": "any-short", "conditions": []map[string]any{{"type": trigger, "comparison": true, "conditionResult": true}}}, "actionFilters": []map[string]any{{"logicType": "all", "conditions": []any{}, "actions": []map[string]any{{"type": "email", "data": map[string]any{"fallthrough_type": "NoOne"}, "config": config}}}}}, nil
}

// sentryWorkflowMatches proves exact name/state/monitor/trigger/email-recipient/frequency from a successful response; ignored or reshaped configuration leaves the external effect UNKNOWN.
// sentryWorkflowMatches доказывает точные name/state/monitor/trigger/email-recipient/frequency успешного ответа; проигнорированная или изменённая конфигурация оставляет effect UNKNOWN.
func sentryWorkflowMatches(workflow sentryWorkflow, input map[string]any, organization sentryOrganization, id string) bool {
	if workflow.OrganizationID != organization.ID || !sentryDecimal(workflow.ID) || id != "" && workflow.ID != id || workflow.Name != input["name"] || workflow.Enabled == nil || *workflow.Enabled != input["enabled"] || len(workflow.DetectorIDs) != 1 || workflow.DetectorIDs[0] != input["detector_id"] || workflow.Config.Frequency == nil {
		return false
	}
	payload, err := sentryWorkflowPayload(input)
	if err != nil || *workflow.Config.Frequency != payload["config"].(map[string]int)["frequency"] {
		return false
	}
	if workflow.Triggers.LogicType != "any-short" || len(workflow.Triggers.Conditions) != 1 {
		return false
	}
	condition := workflow.Triggers.Conditions[0]
	if condition.Type != input["trigger"] || condition.Comparison == nil || !*condition.Comparison || condition.Result == nil || !*condition.Result || len(workflow.ActionFilters) != 1 {
		return false
	}
	filter := workflow.ActionFilters[0]
	if filter.LogicType != "all" || len(filter.Conditions) != 0 || len(filter.Actions) != 1 || filter.Actions[0].Type != "email" || filter.Actions[0].Data.Fallthrough != "NoOne" {
		return false
	}
	kind, target, _ := sentryNotificationTarget(input["notification_target"].(string))
	return filter.Actions[0].Config.TargetType == kind && filter.Actions[0].Config.TargetID == target
}

// executeSentryOrganization requires a separately selected full-organization definition/profile and dispatches only typed shared-release and alert operations; every mutation has its own EACH gate and no transport retry/recovery effect.
// executeSentryOrganization требует отдельно выбранные full-organization definition/profile и исполняет только typed shared-release/alert operations; каждое изменение имеет собственный EACH gate без transport retry/recovery effect.
func (adapter *Adapter) executeSentryOrganization(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string) (Result, error) {
	contract, exists := integrationpackage.CredentialResourcePermissionContractFor(request.DefinitionKey, request.CapabilityKey)
	if !exists || contract.Operation != request.Operation || request.PersonalCredentialAdmission && request.ResourcePermissionProfile != contract.Profile || !integrationpackage.ValidSentrySlug(configuration["organization_slug"]) || configuration["project_slug"] != "" {
		return Result{}, &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
	organization, err := adapter.sentryReadOrganization(ctx, request, capability, configuration)
	if err != nil {
		return Result{}, err
	}
	if capability.Risk != "READ" {
		metadata, err := adapter.sentryToken(ctx, request, capability, configuration)
		if err != nil {
			return Result{}, err
		}
		if !sentryScopeAllows(metadata, request.Operation) {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
	}
	projection, providerRef, err := adapter.sentryOrganizationResource(ctx, request, capability, configuration, organization)
	if err != nil {
		return Result{}, err
	}
	text, err := json.Marshal(projection)
	if err != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return providerResult(request, providerRef, map[string]string{"text": string(text)})
}

// sentryOrganizationResource keeps organization-wide identity independent from project-scoped helpers and derives affected projects from provider state instead of caller-provided authority claims.
// sentryOrganizationResource отделяет organization-wide identity от project-scoped helpers и выводит affected projects из provider state вместо caller-provided authority claims.
func (adapter *Adapter) sentryOrganizationResource(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, organization sentryOrganization) (any, string, error) {
	path := "/api/0/organizations/" + organization.Slug
	providerRef := "sentry-organization:" + organization.ID
	switch request.Operation {
	case "sentry.organization.read":
		return map[string]string{"id": organization.ID, "slug": organization.Slug, "name": organization.Name, "authority_scope": "ORGANIZATION"}, providerRef, nil
	case "sentry.organization.project.list":
		query, limit, err := sentryPageQuery(request.Input)
		if err != nil {
			return nil, "", err
		}
		path += "/projects/"
		response, err := adapter.sentryJSON(ctx, request, capability, configuration, http.MethodGet, path, query, nil)
		if err != nil {
			return nil, "", err
		}
		var projects []struct {
			ID   string `json:"id"`
			Slug string `json:"slug"`
			Name string `json:"name"`
		}
		if integrationpackage.ValidateCredentialPermissionJSON(response.Body) != nil || decodeProviderJSON(response.Body, &projects) != nil || projects == nil || len(projects) > limit {
			return nil, "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		items := []map[string]string{}
		seen := map[string]bool{}
		for _, project := range projects {
			if !sentryDecimal(project.ID) || !integrationpackage.ValidSentrySlug(project.Slug) || project.Name == "" || len(project.Name) > 1000 || seen[project.ID] {
				return nil, "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			}
			seen[project.ID] = true
			items = append(items, map[string]string{"id": project.ID, "slug": project.Slug, "name": project.Name})
		}
		result, err := sentryListResult(response, configuration["base_url"], path, query, items)
		return result, providerRef, err
	case "sentry.organization.release.list", "sentry.organization.release.read", "sentry.organization.release.create", "sentry.organization.release.update":
		return adapter.sentryOrganizationRelease(ctx, request, capability, configuration, organization)
	case "sentry.organization.alert.list", "sentry.organization.alert.read", "sentry.organization.alert.create", "sentry.organization.alert.update", "sentry.organization.alert.delete", "sentry.organization.detector.list", "sentry.organization.detector.read":
		return adapter.sentryOrganizationAlert(ctx, request, capability, configuration, organization)
	default:
		return nil, "", &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
}

// sentryOrganizationRelease reads or changes shared release metadata only under full-organization authority; create explicitly names its project association and never claims atomic project-only ownership.
// sentryOrganizationRelease читает или меняет metadata общего release только с full-organization authority; create явно задаёт project association и не заявляет atomic project-only ownership.
func (adapter *Adapter) sentryOrganizationRelease(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, organization sentryOrganization) (any, string, error) {
	path := "/api/0/organizations/" + organization.Slug + "/releases/"
	query, limit, err := sentryPageQuery(request.Input)
	if err != nil {
		return nil, "", err
	}
	version, _ := request.Input["version"].(string)
	if request.Operation != "sentry.organization.release.list" && !sentryVersion(version) {
		return nil, "", &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	method := http.MethodGet
	var payload any
	mutation := capability.Risk != "READ"
	if mutation {
		ref, _ := request.Input["ref"].(string)
		if !sentryVersion(ref) {
			return nil, "", &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
	}
	if request.Operation == "sentry.organization.release.create" {
		project, _ := request.Input["project_slug"].(string)
		if !integrationpackage.ValidSentrySlug(project) {
			return nil, "", &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		projectConfig := map[string]string{"base_url": configuration["base_url"], "organization_slug": organization.Slug, "project_slug": project}
		resolved, err := adapter.sentryReadProject(ctx, request, capability, projectConfig)
		if err != nil {
			return nil, "", err
		}
		if resolved.Organization.ID != organization.ID {
			return nil, "", &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		method = http.MethodPost
		payload = map[string]any{"version": version, "projects": []string{project}, "status": request.Input["status"], "ref": request.Input["ref"]}
		query = nil
	} else if version != "" {
		path += url.PathEscape(version) + "/"
		query = nil
		if request.Operation == "sentry.organization.release.update" {
			before, err := adapter.sentryJSON(ctx, request, capability, configuration, http.MethodGet, path, nil, nil)
			if err != nil {
				return nil, "", err
			}
			var existing sentryRelease
			if integrationpackage.ValidateCredentialPermissionJSON(before.Body) != nil || decodeProviderJSON(before.Body, &existing) != nil {
				return nil, "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			}
			if _, err := sentryOrganizationReleaseProjection(existing, version); err != nil {
				return nil, "", err
			}
			method = http.MethodPut
			payload = map[string]any{"status": request.Input["status"], "ref": request.Input["ref"]}
		}
	}
	response, err := adapter.sentryJSON(ctx, request, capability, configuration, method, path, query, payload)
	if err != nil {
		return nil, "", err
	}
	invalid := func() (any, string, error) {
		if mutation {
			return nil, "", &UnknownOutcomeError{stage: "response_body"}
		}
		return nil, "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	if integrationpackage.ValidateCredentialPermissionJSON(response.Body) != nil {
		return invalid()
	}
	releases := []sentryRelease{}
	if request.Operation == "sentry.organization.release.list" {
		if decodeProviderJSON(response.Body, &releases) != nil || releases == nil || len(releases) > limit {
			return invalid()
		}
	} else {
		var release sentryRelease
		if decodeProviderJSON(response.Body, &release) != nil {
			return invalid()
		}
		releases = append(releases, release)
	}
	items := []map[string]any{}
	seen := map[string]bool{}
	for _, release := range releases {
		item, err := sentryOrganizationReleaseProjection(release, version)
		if err != nil || seen[release.Version] {
			return invalid()
		}
		seen[release.Version] = true
		if mutation {
			if release.Status != request.Input["status"] || release.Ref == nil || *release.Ref != request.Input["ref"] {
				return invalid()
			}
			if request.Operation == "sentry.organization.release.create" {
				found := false
				for _, member := range release.Projects {
					found = found || member.Slug == request.Input["project_slug"]
				}
				if !found {
					return invalid()
				}
			}
		}
		items = append(items, item)
	}
	if version != "" {
		return items[0], "sentry-release:" + organization.ID + ":" + version, nil
	}
	result, err := sentryListResult(response, configuration["base_url"], path, query, items)
	return result, "sentry-organization:" + organization.ID, err
}

// sentryReadDetector resolves a monitor inside the current organization before it can be bound to a newly configured workflow; caller IDs never establish provider ownership.
// sentryReadDetector разрешает monitor внутри текущей организации перед привязкой к workflow; caller IDs не доказывают provider ownership.
func (adapter *Adapter) sentryReadDetector(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, id string) (sentryDetector, error) {
	if !sentryDecimal(id) {
		return sentryDetector{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	response, err := adapter.sentryJSON(ctx, request, capability, configuration, http.MethodGet, "/api/0/organizations/"+configuration["organization_slug"]+"/detectors/"+id+"/", nil, nil)
	if err != nil {
		return sentryDetector{}, err
	}
	var detector sentryDetector
	if integrationpackage.ValidateCredentialPermissionJSON(response.Body) != nil || decodeProviderJSON(response.Body, &detector) != nil || detector.ID != id || detector.Name == "" || len(detector.Name) > 2000 || detector.Type == "" || len(detector.Type) > 100 || detector.ProjectID != nil && !sentryDecimal(*detector.ProjectID) {
		return sentryDetector{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return detector, nil
}

// sentryOrganizationAlert manages a finite email-only workflow profile and monitor discovery under full-organization authority; delete reports a provider deletion request rather than claiming asynchronous removal has finished.
// sentryOrganizationAlert управляет конечным email-only workflow profile и monitor discovery с full-organization authority; delete сообщает о запросе удаления, не заявляя завершение асинхронного удаления.
func (adapter *Adapter) sentryOrganizationAlert(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, organization sentryOrganization) (any, string, error) {
	base := "/api/0/organizations/" + organization.Slug
	providerRef := "sentry-organization:" + organization.ID
	if request.Operation == "sentry.organization.detector.read" {
		id, _ := request.Input["detector_id"].(string)
		detector, err := adapter.sentryReadDetector(ctx, request, capability, configuration, id)
		if err != nil {
			return nil, "", err
		}
		return map[string]any{"id": detector.ID, "name": detector.Name, "type": detector.Type, "project_id": detector.ProjectID, "includes_all_projects": detector.ProjectID == nil}, "sentry-detector:" + id, nil
	}
	if request.Operation == "sentry.organization.detector.list" {
		query, limit, err := sentryPageQuery(request.Input)
		if err != nil {
			return nil, "", err
		}
		path := base + "/detectors/"
		response, err := adapter.sentryJSON(ctx, request, capability, configuration, http.MethodGet, path, query, nil)
		if err != nil {
			return nil, "", err
		}
		var detectors []sentryDetector
		if integrationpackage.ValidateCredentialPermissionJSON(response.Body) != nil || decodeProviderJSON(response.Body, &detectors) != nil || detectors == nil || len(detectors) > limit {
			return nil, "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		items := []map[string]any{}
		seen := map[string]bool{}
		for _, detector := range detectors {
			if !sentryDecimal(detector.ID) || seen[detector.ID] || detector.Name == "" || len(detector.Name) > 2000 || detector.Type == "" || len(detector.Type) > 100 || detector.ProjectID != nil && !sentryDecimal(*detector.ProjectID) {
				return nil, "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			}
			seen[detector.ID] = true
			items = append(items, map[string]any{"id": detector.ID, "name": detector.Name, "type": detector.Type, "project_id": detector.ProjectID, "includes_all_projects": detector.ProjectID == nil})
		}
		result, err := sentryListResult(response, configuration["base_url"], path, query, items)
		return result, providerRef, err
	}
	path := base + "/workflows/"
	if request.Operation == "sentry.organization.alert.list" {
		query, limit, err := sentryPageQuery(request.Input)
		if err != nil {
			return nil, "", err
		}
		response, err := adapter.sentryJSON(ctx, request, capability, configuration, http.MethodGet, path, query, nil)
		if err != nil {
			return nil, "", err
		}
		var workflows []sentryAlert
		if integrationpackage.ValidateCredentialPermissionJSON(response.Body) != nil || decodeProviderJSON(response.Body, &workflows) != nil || workflows == nil || len(workflows) > limit {
			return nil, "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		items := []map[string]any{}
		seen := map[string]bool{}
		for _, workflow := range workflows {
			if !sentryDecimal(workflow.ID) || seen[workflow.ID] || workflow.OrganizationID != organization.ID || workflow.Name == "" || len(workflow.Name) > 2000 || workflow.Enabled == nil {
				return nil, "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			}
			seen[workflow.ID] = true
			scope, err := adapter.sentryWorkflowScope(ctx, request, capability, configuration, workflow.ID)
			if err != nil {
				return nil, "", err
			}
			items = append(items, map[string]any{"id": workflow.ID, "name": workflow.Name, "enabled": *workflow.Enabled, "affected_scope": scope})
		}
		result, err := sentryListResult(response, configuration["base_url"], path, query, items)
		return result, providerRef, err
	}
	id, _ := request.Input["alert_id"].(string)
	var beforeScope map[string]any
	if request.Operation != "sentry.organization.alert.create" {
		if !sentryDecimal(id) {
			return nil, "", &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		path += id + "/"
		response, err := adapter.sentryJSON(ctx, request, capability, configuration, http.MethodGet, path, nil, nil)
		if err != nil {
			return nil, "", err
		}
		var workflow sentryAlert
		if integrationpackage.ValidateCredentialPermissionJSON(response.Body) != nil || decodeProviderJSON(response.Body, &workflow) != nil || workflow.ID != id || workflow.OrganizationID != organization.ID || workflow.Name == "" || len(workflow.Name) > 2000 || workflow.Enabled == nil {
			return nil, "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		beforeScope, err = adapter.sentryWorkflowScope(ctx, request, capability, configuration, id)
		if err != nil {
			return nil, "", err
		}
		if request.Operation == "sentry.organization.alert.read" {
			return map[string]any{"id": id, "name": workflow.Name, "enabled": *workflow.Enabled, "affected_scope": beforeScope}, "sentry-alert:" + id, nil
		}
	}
	if request.Operation == "sentry.organization.alert.delete" {
		response, err := adapter.sentryJSON(ctx, request, capability, configuration, http.MethodDelete, path, nil, nil)
		if err != nil {
			return nil, "", err
		}
		if response.StatusCode != http.StatusNoContent || len(response.Body) != 0 {
			return nil, "", &UnknownOutcomeError{stage: "response_body"}
		}
		return map[string]any{"id": id, "deletion_requested": true, "observed_before_affected_scope": beforeScope}, "sentry-alert:" + id, nil
	}
	payload, err := sentryWorkflowPayload(request.Input)
	if err != nil {
		return nil, "", err
	}
	detectorID, _ := request.Input["detector_id"].(string)
	if _, err := adapter.sentryReadDetector(ctx, request, capability, configuration, detectorID); err != nil {
		return nil, "", err
	}
	method := http.MethodPost
	if request.Operation == "sentry.organization.alert.update" {
		method = http.MethodPut
	} else if request.Operation != "sentry.organization.alert.create" {
		return nil, "", &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
	response, err := adapter.sentryJSON(ctx, request, capability, configuration, method, path, nil, payload)
	if err != nil {
		return nil, "", err
	}
	var workflow sentryWorkflow
	if integrationpackage.ValidateCredentialPermissionJSON(response.Body) != nil || decodeProviderJSON(response.Body, &workflow) != nil || !sentryWorkflowMatches(workflow, request.Input, organization, id) {
		return nil, "", &UnknownOutcomeError{stage: "response_body"}
	}
	scope, err := adapter.sentryWorkflowScope(ctx, request, capability, configuration, workflow.ID)
	if err != nil {
		return nil, "", &UnknownOutcomeError{stage: "response_body"}
	}
	return map[string]any{"id": workflow.ID, "name": workflow.Name, "enabled": *workflow.Enabled, "detector_id": detectorID, "trigger": request.Input["trigger"], "notification_target": request.Input["notification_target"], "frequency_minutes": *workflow.Config.Frequency, "affected_scope": scope}, "sentry-alert:" + workflow.ID, nil
}
