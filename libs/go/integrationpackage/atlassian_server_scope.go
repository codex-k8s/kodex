package integrationpackage

import (
	"encoding/json"
	"errors"
	"maps"
	"regexp"
	"strings"
)

var (
	// errAtlassianTarget identifies a missing, malformed or unapproved resource selector.
	// errAtlassianTarget обозначает отсутствующий, неверный или несогласованный селектор ресурса.
	errAtlassianTarget = errors.New("invalid Atlassian Server target")
	jiraProjectKey     = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	jiraIssueKey       = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}-[0-9]+$`)
	confluenceSpaceKey = regexp.MustCompile(`^[A-Za-z0-9_~.-]{1,64}$`)
)

// validConfluenceSpaceKey rejects dot segments even though they consist of valid characters.
// validConfluenceSpaceKey отклоняет сегменты-точки, хотя они состоят из разрешённых символов.
func validConfluenceSpaceKey(value string) bool {
	return value != "." && value != ".." && confluenceSpaceKey.MatchString(value)
}

// ResolveAtlassianServerTarget binds the operation to selectors from its canonical approval input.
// It returns an invocation-local configuration; the connection and resource scope are unchanged.
// ResolveAtlassianServerTarget закрепляет операцию за селекторами из канонического ввода согласования.
// Функция возвращает локальную конфигурацию вызова, сохраняя подключение и его область.
func (capability Capability) ResolveAtlassianServerTarget(approval string, configuration map[string]string, input []byte) (map[string]string, error) {
	var target struct {
		Project string `json:"project_key"`
		Issue   string `json:"issue_key"`
		Type    string `json:"issue_type"`
		Space   string `json:"space_key"`
	}
	if err := json.Unmarshal(input, &target); err != nil {
		return nil, errAtlassianTarget
	}
	instance := configuration["scope_kind"] == "INSTANCE"
	write := capability.Risk != "READ"
	if instance && write && approval != "HUMAN_EACH_EFFECT" {
		return nil, errAtlassianTarget
	}

	selector, value, valid := "space_key", target.Space, validConfluenceSpaceKey
	if capability.ResourceScope.Kind == "JIRA_PROJECT" {
		selector, value, valid = "project_key", target.Project, jiraProjectKey.MatchString
		if instance && target.Issue != "" {
			if !jiraIssueKey.MatchString(target.Issue) {
				return nil, errAtlassianTarget
			}
			value, _, _ = strings.Cut(target.Issue, "-")
		}
	}
	if !instance {
		if value != "" && value != configuration[selector] || target.Type != "" && target.Type != configuration["issue_type"] {
			return nil, errAtlassianTarget
		}
		return configuration, nil
	}
	if value == "" && write || value != "" && !valid(value) {
		return nil, errAtlassianTarget
	}
	selected := maps.Clone(configuration)
	if value != "" {
		selected[selector] = value
	}
	if capability.Operation == "jira.issue.create" {
		if target.Type == "" || strings.TrimSpace(target.Type) != target.Type {
			return nil, errAtlassianTarget
		}
		selected["issue_type"] = target.Type
	}
	return selected, nil
}
