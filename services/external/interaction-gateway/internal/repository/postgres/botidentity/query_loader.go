package botidentity

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
var embeddedSQL embed.FS

var (
	queryHeaderPattern = regexp.MustCompile(`^-- name: ([a-z][a-z0-9_]+) :(one|many|exec)$`)
	queryArgPattern    = regexp.MustCompile(`@([a-z][a-z0-9_]*)`)
	positionalPattern  = regexp.MustCompile(`\$[0-9]+`)
)

var expectedQueries = map[string]string{
	"binding__get": "one", "binding__upsert": "exec",
	"catalog_cursor__resolve": "one", "catalog_cursor__upsert": "one",
	"identity__upsert": "one", "operation__accept": "exec", "operation__claim": "one",
	"operation__defer": "one", "operation__finish": "exec", "operation__get": "one",
	"operation__insert": "exec", "operation__lock": "one", "operation__mark_effect": "one",
	"operation__membership": "exec", "operation__reclaim": "exec", "operation__repair": "exec",
	"ownership__available": "one", "ownership__reserve": "one", "ownership__reserve_operation": "one",
	"readiness__check": "one", "readiness__probe_cursor": "one", "runtime__admit": "one",
	"runtime__resolve": "one", "selector__resolve": "one",
	"selector__upsert": "one", "transaction__activate_scope": "exec",
	"watermark__admit": "exec", "watermark__advance": "one", "watermark__close": "exec",
	"work_scope__next": "one",
}

func validateQueries() error {
	entries, err := fs.ReadDir(embeddedSQL, "sql")
	if err != nil {
		return errors.New("read Agent bot identity SQL corpus")
	}
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			return fmt.Errorf("unknown Agent bot identity SQL entry %q", entry.Name())
		}
		raw, err := fs.ReadFile(embeddedSQL, "sql/"+entry.Name())
		if err != nil {
			return errors.New("read Agent bot identity SQL query")
		}
		lines := strings.Split(string(raw), "\n")
		if len(lines) < 3 {
			return fmt.Errorf("Agent bot identity SQL header is missing in %q", entry.Name())
		}
		header := queryHeaderPattern.FindStringSubmatch(lines[0])
		if len(header) != 3 || !strings.HasPrefix(lines[1], "-- params:") ||
			header[1] != strings.TrimSuffix(entry.Name(), ".sql") || expectedQueries[header[1]] != header[2] {
			return fmt.Errorf("Agent bot identity SQL header is invalid in %q", entry.Name())
		}
		body := strings.Join(lines[2:], "\n")
		if strings.Count(body, ";") != 1 || !strings.HasSuffix(strings.TrimSpace(body), ";") ||
			positionalPattern.MatchString(body) {
			return fmt.Errorf("Agent bot identity SQL body is invalid in %q", entry.Name())
		}
		declared := strings.TrimSpace(strings.TrimPrefix(lines[1], "-- params:"))
		declaredParams := []string(nil)
		if declared != "" {
			declaredParams = strings.Split(strings.ReplaceAll(declared, "@", ""), ",")
		}
		used := []string{}
		for _, match := range queryArgPattern.FindAllStringSubmatch(body, -1) {
			if !slices.Contains(used, match[1]) {
				used = append(used, match[1])
			}
		}
		slices.Sort(declaredParams)
		slices.Sort(used)
		if !slices.Equal(declaredParams, used) {
			return fmt.Errorf("Agent bot identity SQL parameter mismatch in %q", entry.Name())
		}
		seen[header[1]] = struct{}{}
	}
	if len(seen) != len(expectedQueries) {
		return errors.New("Agent bot identity SQL corpus is incomplete")
	}
	return nil
}
