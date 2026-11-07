package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// gitLabPermissionVersion accepts the documented19.x metadata contract starting19.2, when
// detailed granular scopes became observable. New majors and ambiguous suffixes stay unsupported.
// gitLabPermissionVersion допускает документированный metadata contract19.x начиная с19.2,
// когда появились подробные granular scopes. Новые major и неоднозначные suffix не допускаются.
var gitLabPermissionVersion = regexp.MustCompile(`^19\.([0-9]{1,2})\.[0-9]{1,4}(?:-ee|-ce)?$`)

// requireGitLabPersonalWritePermission checks current identity, explicit token granularity and
// exact project membership/features before a registered personal write. These checks confer no
// lasting WRITE proof and do not bypass the external API's final authorization or Human Gate.
// requireGitLabPersonalWritePermission проверяет текущую identity, явный token granularity и
// права/функции точного проекта перед registered personal write. Долговременный WRITE proof не
// создаётся; окончательная авторизация внешнего API и Human Gate не обходятся.
func (adapter *Adapter) requireGitLabPersonalWritePermission(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, canonicalInput []byte) error {
	if !request.PersonalCredentialAdmission {
		return nil
	}
	permission := ""
	switch request.ResourcePermissionProfile {
	case integrationpackage.CredentialResourcePermissionGitLabIssueCreateV1:
		permission = "create_issue"
	case integrationpackage.CredentialResourcePermissionGitLabBranchCreateV1:
		permission = "create_branch"
	case integrationpackage.CredentialResourcePermissionGitLabMergeRequestCreateV1:
		permission = "create_merge_request"
	default:
		return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	metadata, err := adapter.readGitLabWriteMetadata(ctx, request, capability, configuration)
	if err != nil {
		return err
	}
	token, project, projectID := metadata.token, metadata.project, metadata.projectID
	if !gitLabPermissionBool(project, "archived", false) || !gitLabProjectMembershipAllowsWrite(project) || !gitLabTokenScopeAllowsWrite(token, projectID, permission) {
		return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	for _, key := range []string{"marked_for_deletion_at", "marked_for_deletion_on"} {
		if value, present := project[key]; present && string(bytes.TrimSpace(value)) != "null" {
			return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
	}
	projectPath := adapter.gitLabProjectPath(configuration)
	read := func(path string) (map[string]json.RawMessage, error) {
		body, err := adapter.gitLabPermissionGET(ctx, request, capability, configuration, path, nil)
		if err != nil {
			return nil, err
		}
		return gitLabPermissionObject(body)
	}
	features := []string{"repository_access_level"}
	if permission == "create_issue" {
		features = []string{"issues_access_level"}
	} else if permission == "create_merge_request" {
		features = append(features, "merge_requests_access_level")
	}
	for _, key := range features {
		level, _ := gitLabPermissionString(project, key)
		if level != "enabled" && level != "private" {
			return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
	}
	if permission == "create_branch" {
		var branch struct{ Branch, Ref string }
		if decodeProviderJSON(canonicalInput, &branch) != nil || branch.Branch == "" || branch.Ref == "" {
			return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		return adapter.requireGitLabUnprotectedBranch(ctx, request, capability, configuration, branch.Branch)
	}
	if permission == "create_merge_request" {
		var branches struct {
			Source string `json:"source_branch"`
			Target string `json:"target_branch"`
		}
		if decodeProviderJSON(canonicalInput, &branches) != nil || branches.Source == "" || branches.Target == "" || branches.Source == branches.Target {
			return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		for _, branchName := range []string{branches.Source, branches.Target} {
			branch, err := read(projectPath + "/repository/branches/" + url.PathEscape(branchName))
			if err != nil {
				return err
			}
			name, _ := gitLabPermissionString(branch, "name")
			if name != branchName {
				return &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			}
		}
	}
	return nil
}

// gitLabWriteMetadata contains only in-process current metadata. Raw identities and scopes are
// never serialized into owner observations or tool results.
// gitLabWriteMetadata содержит только текущие in-process metadata. Raw identities и scopes
// не сериализуются в owner observations либо tool results.
type gitLabWriteMetadata struct {
	token, project    map[string]json.RawMessage
	projectID, userID int64
	expiresAt         *time.Time
}

// readGitLabWriteMetadata shares fixed current token/project GETs between verification and
// execution, without retries, arbitrary IDs, redirects or provider payload publication.
// readGitLabWriteMetadata объединяет фиксированные current token/project GET для verification и
// execution без retries, произвольных IDs, redirects либо публикации provider payload.
func (adapter *Adapter) readGitLabWriteMetadata(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string) (gitLabWriteMetadata, error) {
	read := func(path string) (map[string]json.RawMessage, error) {
		body, err := adapter.gitLabPermissionGET(ctx, request, capability, configuration, path, nil)
		if err != nil {
			return nil, err
		}
		return gitLabPermissionObject(body)
	}
	version, err := read("/api/v4/version")
	if err != nil {
		return gitLabWriteMetadata{}, err
	}
	versionName, _ := gitLabPermissionString(version, "version")
	parts := gitLabPermissionVersion.FindStringSubmatch(versionName)
	if len(parts) != 2 {
		return gitLabWriteMetadata{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	minor, _ := strconv.Atoi(parts[1])
	if minor < 2 {
		return gitLabWriteMetadata{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	user, err := read("/api/v4/user")
	if err != nil {
		return gitLabWriteMetadata{}, err
	}
	userID, valid := gitLabPermissionID(user, "id")
	userState, _ := gitLabPermissionString(user, "state")
	if !valid || userState != "active" {
		return gitLabWriteMetadata{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	token, err := read("/api/v4/personal_access_tokens/self")
	if err != nil {
		return gitLabWriteMetadata{}, err
	}
	tokenID, valid := gitLabPermissionID(token, "id")
	if valid && gitLabPersonalTokenExplicitlyUnusable(token, userID, time.Now()) {
		return gitLabWriteMetadata{}, &SafeError{Code: "INTEGRATION_AUTH_REJECTED", AuthenticationHTTPStatus: http.StatusUnauthorized}
	}
	if !valid || !gitLabPersonalTokenCurrent(token, userID, time.Now()) {
		return gitLabWriteMetadata{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	if gitLabPermissionBool(token, "granular", true) {
		// Only the authenticated token's own ID may select this fixed metadata endpoint.
		// Фиксированный metadata endpoint получает только собственный ID authenticated token.
		token, err = read("/api/v4/personal_access_tokens/" + strconv.FormatInt(tokenID, 10))
		if err != nil {
			return gitLabWriteMetadata{}, err
		}
		currentID, valid := gitLabPermissionID(token, "id")
		if valid && currentID == tokenID && gitLabPersonalTokenExplicitlyUnusable(token, userID, time.Now()) {
			return gitLabWriteMetadata{}, &SafeError{Code: "INTEGRATION_AUTH_REJECTED", AuthenticationHTTPStatus: http.StatusUnauthorized}
		}
		if !valid || currentID != tokenID || !gitLabPermissionBool(token, "granular", true) || !gitLabPersonalTokenCurrent(token, userID, time.Now()) {
			return gitLabWriteMetadata{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
	}
	projectPath := adapter.gitLabProjectPath(configuration)
	project, err := read(projectPath)
	if err != nil {
		return gitLabWriteMetadata{}, err
	}
	projectID, valid := gitLabPermissionID(project, "id")
	projectName, _ := gitLabPermissionString(project, "path_with_namespace")
	if !valid || projectName != configuration["project_path"] || (!gitLabPermissionBool(project, "archived", false) && !gitLabPermissionBool(project, "archived", true)) {
		return gitLabWriteMetadata{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}

	var expiry *string
	_ = json.Unmarshal(token["expires_at"], &expiry)
	observation := gitLabWriteMetadata{token: token, project: project, projectID: projectID, userID: userID}
	if expiry != nil {
		deadline, _ := time.Parse("2006-01-02", *expiry)
		observation.expiresAt = &deadline
	}
	return observation, nil
}

// gitLabPermissionGET uses the existing HTTPS/auth/SSRF boundary without redirects or retries.
// 403 is an observed metadata refusal, while401 retains the exact-key revalidation lifecycle.
// gitLabPermissionGET использует прежнюю HTTPS/auth/SSRF границу без redirects или retries.
// 403 означает отказ metadata, тогда как401 сохраняет перепроверку exact key.
func (adapter *Adapter) gitLabPermissionGET(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, path string, query url.Values) ([]byte, error) {
	body, err := adapter.gitLabJSON(ctx, request, capability, configuration, http.MethodGet, path, query, nil, "")
	var safe *SafeError
	if errors.As(err, &safe) && safe.Code == "INTEGRATION_AUTH_REJECTED" && safe.AuthenticationHTTPStatus == http.StatusForbidden {
		return nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	return body, err
}

// gitLabPermissionObject rejects duplicate/folded keys, excessive payloads and non-object
// metadata before any typed permission decision; raw payloads never become a tool result.
// gitLabPermissionObject отклоняет duplicate/folded keys, избыточные payload и не-object
// metadata до typed решения о правах; raw payload никогда не становится tool result.
func gitLabPermissionObject(body []byte) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if integrationpackage.ValidateCredentialPermissionJSON(body) != nil || json.Unmarshal(body, &fields) != nil || fields == nil {
		return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return fields, nil
}

// gitLabPermissionString reads a canonical field name and bounded nonempty string only.
// gitLabPermissionString читает только canonical field name и ограниченную непустую строку.
func gitLabPermissionString(fields map[string]json.RawMessage, key string) (string, bool) {
	var value string
	err := json.Unmarshal(fields[key], &value)
	return value, err == nil && value != "" && len(value) <= 2048
}

// gitLabPermissionBool requires an explicit correctly typed boolean, excluding null/defaults.
// gitLabPermissionBool требует явный корректный boolean, исключая null и defaults.
func gitLabPermissionBool(fields map[string]json.RawMessage, key string, want bool) bool {
	var value *bool
	return json.Unmarshal(fields[key], &value) == nil && value != nil && *value == want
}

// gitLabPermissionID accepts a positive exact JSON int64, never a float or prompt-selected ID.
// gitLabPermissionID допускает положительный exact JSON int64, не float или ID из prompt.
func gitLabPermissionID(fields map[string]json.RawMessage, key string) (int64, bool) {
	var value int64
	err := json.Unmarshal(fields[key], &value)
	return value, err == nil && value > 0
}

// gitLabPersonalTokenCurrent checks only the current token/user metadata, requiring explicit
// active/revoked/granular facts and a known UTC expiry. It never exports token identity or scopes.
// gitLabPersonalTokenCurrent проверяет metadata только текущего token/user, требуя явные
// active/revoked/granular facts и известный UTC expiry. Identity или scopes токена не выдаются.
func gitLabPersonalTokenCurrent(token map[string]json.RawMessage, userID int64, now time.Time) bool {
	ownerID, valid := gitLabPermissionID(token, "user_id")
	if !valid || ownerID != userID || !gitLabPermissionBool(token, "active", true) || !gitLabPermissionBool(token, "revoked", false) ||
		(!gitLabPermissionBool(token, "granular", true) && !gitLabPermissionBool(token, "granular", false)) {
		return false
	}
	var expiry *string
	if json.Unmarshal(token["expires_at"], &expiry) != nil {
		return false
	}
	if expiry != nil {
		deadline, err := time.Parse("2006-01-02", *expiry)
		if err != nil || !deadline.After(now.UTC()) {
			return false
		}
	}
	return true
}

// gitLabPersonalTokenExplicitlyUnusable recognizes only the authenticated token owner's explicit
// revoked/inactive or elapsed expiry metadata. Unknown fields never become an invented refusal.
// gitLabPersonalTokenExplicitlyUnusable распознаёт только явный revoked/inactive либо истёкший expiry
// authenticated token owner. Неизвестные поля не превращаются в выдуманный отказ.
func gitLabPersonalTokenExplicitlyUnusable(token map[string]json.RawMessage, userID int64, now time.Time) bool {
	ownerID, valid := gitLabPermissionID(token, "user_id")
	if !valid || ownerID != userID {
		return false
	}
	if gitLabPermissionBool(token, "active", false) || gitLabPermissionBool(token, "revoked", true) {
		return true
	}
	var expiry string
	if json.Unmarshal(token["expires_at"], &expiry) == nil {
		deadline, err := time.Parse("2006-01-02", expiry)
		return err == nil && !deadline.After(now.UTC())
	}
	return false
}

// gitLabProjectMembershipAllowsWrite conservatively requires an explicit base role whose
// documented permissions include the three registered writes; unknown/custom-only roles close.
// gitLabProjectMembershipAllowsWrite консервативно требует явную base role с documented
// правами трёх registered writes; unknown либо custom-only роли не допускаются.
func gitLabProjectMembershipAllowsWrite(project map[string]json.RawMessage) bool {
	var permissions map[string]json.RawMessage
	if json.Unmarshal(project["permissions"], &permissions) != nil {
		return false
	}
	for _, key := range []string{"project_access", "group_access"} {
		var membership map[string]json.RawMessage
		if json.Unmarshal(permissions[key], &membership) == nil {
			level, valid := gitLabPermissionID(membership, "access_level")
			if valid && (level == 30 || level == 40 || level == 50) {
				return true
			}
		}
	}
	return false
}

// gitLabTokenScopeAllowsWrite distinguishes legacy api from granular project/action scopes.
// Only exact project selection or proven all-memberships is supported; group/personal scope
// ancestry is not guessed, and an unknown/missing scope never grants a write.
// gitLabTokenScopeAllowsWrite разделяет legacy api и granular project/action scopes. Допускаются
// только exact project selection либо проверенное all-memberships; group/personal ancestry не
// угадывается, а unknown либо отсутствующий scope никогда не выдаёт запись.
func gitLabTokenScopeAllowsWrite(token map[string]json.RawMessage, projectID int64, permission string) bool {
	if gitLabPermissionBool(token, "granular", false) {
		var scopes []string
		if json.Unmarshal(token["scopes"], &scopes) != nil || len(scopes) == 0 || len(scopes) > 64 {
			return false
		}
		if raw, present := token["granular_scopes"]; present {
			var unexpected []json.RawMessage
			if json.Unmarshal(raw, &unexpected) != nil || len(unexpected) != 0 {
				return false
			}
		}
		allowed, seen := false, map[string]bool{}
		for _, scope := range scopes {
			if scope == "" || len(scope) > 128 || seen[scope] {
				return false
			}
			seen[scope], allowed = true, allowed || scope == "api"
		}
		return allowed
	}
	if !gitLabPermissionBool(token, "granular", true) {
		return false
	}
	var scopes []map[string]json.RawMessage
	if json.Unmarshal(token["granular_scopes"], &scopes) != nil || len(scopes) == 0 || len(scopes) > 128 {
		return false
	}
	allowed := false
	for _, scope := range scopes {
		access, accessOK := gitLabPermissionString(scope, "access")
		var permissions []string
		if !accessOK || json.Unmarshal(scope["permissions"], &permissions) != nil || len(permissions) == 0 || len(permissions) > 128 {
			return false
		}
		selected, selectedOK := gitLabPermissionID(scope, "project_id")
		groupIsNull := string(bytes.TrimSpace(scope["group_id"])) == "null"
		projectIsNull := string(bytes.TrimSpace(scope["project_id"])) == "null"
		_, groupOK := gitLabPermissionID(scope, "group_id")
		if (!projectIsNull && !selectedOK) || (!groupIsNull && !groupOK) || (!projectIsNull && !groupIsNull) {
			return false
		}
		eligible := groupIsNull && ((access == "selected_memberships" && selectedOK && selected == projectID) || (access == "all_memberships" && projectIsNull))
		seen := map[string]bool{}
		for _, granted := range permissions {
			if granted == "" || len(granted) > 128 || seen[granted] {
				return false
			}
			seen[granted], allowed = true, allowed || eligible && granted == permission
		}
	}
	return allowed
}

// requireGitLabUnprotectedBranch exhausts a bounded protected-rule listing, including inherited
// entries. Any matching rule or unknown wildcard syntax closes creation; no protection is edited.
// requireGitLabUnprotectedBranch исчерпывает ограниченный список protected rules, включая
// inherited entries. Совпавшее правило либо unknown wildcard syntax закрывают создание; защита не меняется.
func (adapter *Adapter) requireGitLabUnprotectedBranch(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, branch string) error {
	if branch == "" || len(branch) > 255 || strings.ContainsAny(branch, "\r\n\x00") {
		return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	for page := 1; page <= 5; page++ {
		body, err := adapter.gitLabPermissionGET(ctx, request, capability, configuration, adapter.gitLabProjectPath(configuration)+"/protected_branches",
			url.Values{"per_page": {"100"}, "page": {strconv.Itoa(page)}})
		if err != nil {
			return err
		}
		var rules []map[string]json.RawMessage
		if integrationpackage.ValidateCredentialPermissionJSON(body) != nil || string(bytes.TrimSpace(body)) == "null" || json.Unmarshal(body, &rules) != nil || len(rules) > 100 {
			return &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		for _, rule := range rules {
			pattern, valid := gitLabPermissionString(rule, "name")
			if !valid || len(pattern) > 255 || strings.ContainsAny(pattern, "?[]{}\\\r\n\x00") {
				return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
			}
			expression := "^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, ".*") + "$"
			matched, err := regexp.MatchString(expression, branch)
			if err != nil || matched {
				return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
			}
		}
		if len(rules) < 100 {
			return nil
		}
	}
	return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
}
