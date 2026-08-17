package team

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

//go:embed sql/*.sql
var embeddedTeamSQL embed.FS

var (
	queryHeaderPattern = regexp.MustCompile(`^-- name: ([a-z][a-z0-9_]+) :(one|many|exec)$`)
	queryArgPattern    = regexp.MustCompile(`@([a-z][a-z0-9_]*)`)
	positionalPattern  = regexp.MustCompile(`\$[0-9]+`)
)

var expectedQueries = map[string]string{
	"mattermost_runtime_checkpoint__admission":    "one",
	"mattermost_runtime_checkpoint__upsert":       "exec",
	"mattermost_runtime_route__delete_project":    "exec",
	"mattermost_runtime_route__delivery":          "one",
	"mattermost_runtime_route__insert":            "exec",
	"mattermost_runtime_route__list":              "many",
	"mattermost_runtime_route__lock_project":      "one",
	"mattermost_runtime_route__resolve":           "one",
	"mattermost_runtime_route__scope":             "one",
	"team_catalog_cursor__resolve":                "one",
	"team_catalog_cursor__upsert":                 "one",
	"team_create_fence__accept":                   "exec",
	"team_create_fence__acquire":                  "exec",
	"team_create_fence__lock":                     "one",
	"team_create_fence__replace_unlinked":         "exec",
	"team_create_fence__terminal":                 "exec",
	"team_create_fence__unlink":                   "exec",
	"team_operation__accept":                      "exec",
	"team_operation__claim_recovery":              "one",
	"team_operation__get":                         "one",
	"team_operation__insert":                      "exec",
	"team_operation__lock":                        "one",
	"team_operation__mark_ambiguous":              "one",
	"team_operation__mark_effect":                 "one",
	"team_operation__mark_repair":                 "exec",
	"team_operation__reclaim":                     "exec",
	"team_provider_watermark__advance":            "one",
	"team_readiness__check":                       "one",
	"team_readiness__probe_cursor":                "one",
	"team_selector__resolve":                      "one",
	"team_selector__upsert":                       "one",
	"team_work_scope__next":                       "one",
	"transaction__activate_scope":                 "exec",
	"workspace_mapping_operation__claim_recovery": "one",
	"workspace_mapping_operation__get":            "one",
	"workspace_mapping_operation__insert":         "exec",
	"workspace_mapping_operation__lock":           "one",
	"workspace_mapping_operation__mark_ambiguous": "one",
	"workspace_mapping_operation__mark_repair":    "exec",
	"workspace_mapping_operation__mark_terminal":  "exec",
	"workspace_mapping_operation__prepare":        "exec",
	"workspace_mapping_operation__reclaim":        "exec",
	"workspace_mapping_work_scope__next":          "one",
}

func validateTeamQueries() error {
	return validateQueryCorpus(embeddedTeamSQL, expectedQueries)
}

func validateQueryCorpus(source fs.FS, expected map[string]string) error {
	entries, err := fs.ReadDir(source, "sql")
	if err != nil {
		return errors.New("read Mattermost team SQL corpus")
	}
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			return fmt.Errorf("unknown Mattermost team SQL entry %q", entry.Name())
		}
		raw, readErr := fs.ReadFile(source, "sql/"+entry.Name())
		if readErr != nil {
			return errors.New("read Mattermost team SQL query")
		}
		lines := strings.Split(string(raw), "\n")
		if len(lines) < 3 {
			return fmt.Errorf("Mattermost team SQL header is missing in %q", entry.Name())
		}
		header := queryHeaderPattern.FindStringSubmatch(lines[0])
		if len(header) != 3 || lines[1] == "" || !strings.HasPrefix(lines[1], "-- params:") {
			return fmt.Errorf("Mattermost team SQL header is invalid in %q", entry.Name())
		}
		name, cardinality := header[1], header[2]
		if name != strings.TrimSuffix(entry.Name(), ".sql") {
			return fmt.Errorf("Mattermost team SQL name mismatch in %q", entry.Name())
		}
		expectedCardinality, known := expected[name]
		if !known || expectedCardinality != cardinality {
			return fmt.Errorf("Mattermost team SQL query %q is unknown or has invalid cardinality", name)
		}
		if _, duplicate := seen[name]; duplicate {
			return fmt.Errorf("Mattermost team SQL query %q is duplicated", name)
		}
		seen[name] = struct{}{}
		body := strings.Join(lines[2:], "\n")
		if strings.Count(body, ";") != 1 || !strings.HasSuffix(strings.TrimSpace(body), ";") {
			return fmt.Errorf("Mattermost team SQL file %q does not contain exactly one query", name)
		}
		if positionalPattern.MatchString(body) {
			return fmt.Errorf("Mattermost team SQL query %q uses positional parameters", name)
		}
		declared := strings.TrimSpace(strings.TrimPrefix(lines[1], "-- params:"))
		declaredParams := []string(nil)
		if declared != "" {
			declaredParams = strings.Split(strings.ReplaceAll(declared, "@", ""), ",")
		}
		usedParams := make([]string, 0)
		for _, match := range queryArgPattern.FindAllStringSubmatch(body, -1) {
			if !slices.Contains(usedParams, match[1]) {
				usedParams = append(usedParams, match[1])
			}
		}
		slices.Sort(declaredParams)
		slices.Sort(usedParams)
		if !slices.Equal(declaredParams, usedParams) {
			return fmt.Errorf("Mattermost team SQL parameter mismatch in %q", name)
		}
	}
	if len(seen) != len(expected) {
		return errors.New("Mattermost team SQL corpus is incomplete")
	}
	return nil
}
