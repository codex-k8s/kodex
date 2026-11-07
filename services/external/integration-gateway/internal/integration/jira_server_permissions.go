package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// requireJiraServerWritePermission checks one registered current-user permission in its exact
// project or issue context. The fixed Server endpoint receives neither Cloud-only selectors nor
// caller-provided permission names. Unknown/denied metadata closes the write without global proof.
// requireJiraServerWritePermission проверяет одно зарегистрированное право текущего пользователя
// в точном project/issue context. Фиксированный Server endpoint не принимает Cloud selectors либо
// caller-provided имена прав. Unknown/denied metadata закрывают запись без глобального proof.
func (adapter *Adapter) requireJiraServerWritePermission(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, issueKey string) error {
	if !request.PersonalCredentialAdmission {
		return nil
	}
	permission := ""
	query := url.Values{}
	switch request.ResourcePermissionProfile {
	case integrationpackage.CredentialResourcePermissionJiraIssueCreateV1:
		if issueKey != "" {
			return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		permission, query["projectKey"] = "CREATE_ISSUES", []string{configuration["project_key"]}
	case integrationpackage.CredentialResourcePermissionJiraCommentWriteV1:
		permission = "ADD_COMMENTS"
	case integrationpackage.CredentialResourcePermissionJiraTransitionApplyV1:
		permission = "TRANSITION_ISSUES"
	default:
		return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	if permission != "CREATE_ISSUES" {
		if !jiraIssueInProject(issueKey, configuration["project_key"]) {
			return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		query["issueKey"] = []string{issueKey}
	}
	body, err := adapter.jiraServerJSON(ctx, request, capability, configuration, http.MethodGet, "/mypermissions", query, nil, "")
	if err != nil {
		return err
	}
	if integrationpackage.ValidateCredentialPermissionJSON(body) != nil {
		return &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	var root map[string]json.RawMessage
	var permissions map[string]json.RawMessage
	if json.Unmarshal(body, &root) != nil || json.Unmarshal(root["permissions"], &permissions) != nil || len(permissions) == 0 || len(permissions) > 1024 {
		return &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(permissions[permission], &fields) != nil {
		return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	var key, kind string
	var allowed *bool
	var deprecated bool
	if json.Unmarshal(fields["key"], &key) != nil || key != permission || json.Unmarshal(fields["type"], &kind) != nil || kind != "PROJECT" ||
		json.Unmarshal(fields["havePermission"], &allowed) != nil || allowed == nil {
		return &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	if raw, present := fields["deprecatedKey"]; present && json.Unmarshal(raw, &deprecated) != nil {
		return &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	if deprecated || !*allowed {
		return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	return nil
}
