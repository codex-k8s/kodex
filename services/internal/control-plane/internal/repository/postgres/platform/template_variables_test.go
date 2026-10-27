package platform

import (
	"bytes"
	"errors"
	"slices"
	"testing"
	"text/template"

	domainerrs "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

// TestTemplateVariableCatalogContainsOnlyMaterializedNamespaces checks catalog names are
// sorted/unique, required materialized namespaces exist and the unmaterialized runtime.config.ref
// is absent. This tests the declared catalog contract, not whether every variable is available for
// a particular actor or live revision.
// TestTemplateVariableCatalogContainsOnlyMaterializedNamespaces проверяет sorted/unique catalog
// names, обязательные materialized namespaces и отсутствие unmaterialized runtime.config.ref.
// Проверяет объявленный catalog contract, а не доступность каждой variable конкретному actor и
// live revision.
func TestTemplateVariableCatalogContainsOnlyMaterializedNamespaces(t *testing.T) {
	catalog := templateVariableCatalog()
	names := make([]string, 0, len(catalog))
	for _, item := range catalog {
		names = append(names, item.Name)
	}
	if !slices.IsSorted(names) {
		t.Fatalf("template variable catalog is not cursor-safe: %v", names)
	}
	for index := 1; index < len(names); index++ {
		if names[index] == names[index-1] {
			t.Fatalf("template variable catalog contains duplicate %q", names[index])
		}
	}
	for _, forbidden := range []string{"runtime.config.ref"} {
		if slices.Contains(names, forbidden) {
			t.Fatalf("unmaterialized variable %q is advertised", forbidden)
		}
	}
	for _, required := range []string{
		"agent.ref", "agent.name", "user.ref", "organization.ref", "gate.files", "input.files", "session.files", "run.files", "workflow.files", "project.files",
		"project.ref", "project.name", "run.ref", "session.ref", "turn.ref", "runtime.environment.ref",
		"runtime.environment.image.reference", "runtime.environment.image.digest", "runtime.environment.tools",
	} {
		if !slices.Contains(names, required) {
			t.Fatalf("materialized variable %q is missing", required)
		}
	}
}

// TestTemplateVariableCatalogExamplesRenderAgainstCanonicalShape parses every catalog example with
// missingkey=error and requires nonempty rendering against a synthetic canonical variable shape.
// It validates example syntax/data shape only; it neither reads real file scopes nor establishes
// runtime materialization provenance.
// TestTemplateVariableCatalogExamplesRenderAgainstCanonicalShape разбирает каждый catalog example
// с missingkey=error и требует nonempty rendering по synthetic canonical variable shape. Проверяет
// лишь example syntax/data shape; не читает реальные file scopes и не устанавливает runtime
// materialization provenance.
func TestTemplateVariableCatalogExamplesRenderAgainstCanonicalShape(t *testing.T) {
	file := map[string]any{"name": "example.txt", "path": "/workspace/input/example.txt"}
	fileScope := map[string]any{"files": []map[string]any{file}, "files_count": 1,
		"files_dir": "/workspace/input", "manifest_path": "/workspace/input/manifest.json", "values": map[string]any{"title": "Example"}}
	variables := map[string]any{
		"agent":        map[string]any{"ref": "agt_example", "name": "Агент"},
		"automation":   map[string]any{"ref": "sch_example", "name": "Automation", "task": "Task", "scheduled_at": "2026-09-07T12:00:00Z", "timezone": "UTC", "revision": "srev_example"},
		"environment":  map[string]any{"ref": "renv_example"},
		"gate":         fileScope,
		"input":        fileScope,
		"integrations": map[string]any{"summary": "GitHub API", "items": []map[string]any{{"ref": "igr_example", "name": "GitHub API", "version": 1, "description": "Read approved repository", "capability": "github.read"}}},
		"step":         map[string]any{"key": "step", "name": "Analysis", "purpose": "Analyze inputs", "expected_result": "Summary"},
		"node":         map[string]any{"ref": "node_example"},
		"organization": map[string]any{"ref": "org_example", "name": "Организация"},
		"project": map[string]any{"ref": "prj_example", "name": "Проект", "files": []map[string]any{file}, "files_count": 1,
			"files_dir": "/workspace/knowledge", "manifest_path": "/workspace/input/manifest.json"},
		"run": map[string]any{"ref": "run_example", "files": []map[string]any{file}, "files_count": 1,
			"files_dir": "/workspace/input", "manifest_path": "/workspace/input/manifest.json"},
		"runtime": map[string]any{"environment": map[string]any{"ref": "renv_example",
			"image": map[string]any{"reference": "registry.example/runtime@sha256:example", "digest": "sha256:example"},
			"tools": []map[string]any{{"name": "GitHub CLI", "description": "GitHub API"}}}},
		"session": map[string]any{"ref": "ses_example", "files": []map[string]any{file}, "files_count": 1,
			"files_dir": "/workspace", "manifest_path": "/workspace/input/manifest.json"},
		"target": map[string]any{"ref": "agt_example"},
		"task":   "Задача",
		"tools":  map[string]any{"summary": "GitHub CLI"},
		"turn":   map[string]any{"ref": "trn_example"},
		"user":   map[string]any{"ref": "usr_example", "name": "Пользователь"},
		"workflow": map[string]any{"ref": "wfl_example", "name": "Analysis", "purpose": "Analyze inputs", "stage": map[string]any{"key": "step"},
			"files": []map[string]any{file}, "files_count": 1, "files_dir": "/workspace/input", "manifest_path": "/workspace/input/manifest.json"},
	}
	for _, item := range templateVariableCatalog() {
		parsed, err := template.New(item.Name).Option("missingkey=error").Parse(item.Example)
		if err != nil {
			t.Fatalf("parse example for %s: %v", item.Name, err)
		}
		var output bytes.Buffer
		if err := parsed.Execute(&output, variables); err != nil || output.Len() == 0 {
			t.Fatalf("render example for %s: output=%q err=%v", item.Name, output.String(), err)
		}
	}
}

// TestTemplateVariableCursorIsBoundToCatalogFilter round-trips the exact template-variable cursor
// and rejects changed project/query, target context, source kind or actor. It verifies codec
// filter binding with synthetic scope values, not row-level authorization or a fresh catalog read.
// TestTemplateVariableCursorIsBoundToCatalogFilter проверяет round-trip exact template-variable
// cursor и отказ при смене project/query, target context, source kind и actor. Проверяет codec
// filter binding на synthetic scope values, а не row-level authorization и fresh catalog read.
func TestTemplateVariableCursorIsBoundToCatalogFilter(t *testing.T) {
	current := scope{organizationID: "organization", actorID: "actor"}
	filter := query.Filter{ProjectRef: "prj_example", Query: "agent", TemplateContext: &query.TemplateVariableContext{AgentRef: "agt_example"}}
	filter.Page.Token = encodeCatalogCursor(current, "TEMPLATE_VARIABLE", filter, "agent.ref")
	cursor, err := decodeCatalogCursor(current, "TEMPLATE_VARIABLE", filter)
	if err != nil || cursor != "agent.ref" {
		t.Fatalf("decode template variable cursor: cursor=%#v err=%v", cursor, err)
	}
	for _, changed := range []struct{ project, query string }{{"prj_other", "agent"}, {"prj_example", "project"}} {
		other := filter
		other.ProjectRef, other.Query = changed.project, changed.query
		if _, err := decodeCatalogCursor(current, "TEMPLATE_VARIABLE", other); !errors.Is(err, domainerrs.ErrInvalid) {
			t.Fatalf("template variable cursor accepted another filter: %#v err=%v", changed, err)
		}
	}
	other := filter
	other.TemplateContext = &query.TemplateVariableContext{AgentRef: "agt_other"}
	if _, err := decodeCatalogCursor(current, "TEMPLATE_VARIABLE", other); !errors.Is(err, domainerrs.ErrInvalid) {
		t.Fatal("changed target cursor accepted")
	}
	other = filter
	other.SourceKind = "PROJECT"
	if _, err := decodeCatalogCursor(current, "TEMPLATE_VARIABLE", other); !errors.Is(err, domainerrs.ErrInvalid) {
		t.Fatal("changed source cursor accepted")
	}
	current.actorID = "foreign"
	if _, err := decodeCatalogCursor(current, "TEMPLATE_VARIABLE", filter); !errors.Is(err, domainerrs.ErrInvalid) {
		t.Fatal("changed actor cursor accepted")
	}
}

// TestTemplateVariableSourceKindIsClosed checks the documented empty/uppercase source-kind set and
// rejects lowercase, unknown/system and whitespace variants. It exercises a pure classification
// predicate without granting access to any namespace.
// TestTemplateVariableSourceKindIsClosed проверяет объявленный набор empty/uppercase source kinds
// и отклоняет lowercase, unknown/system и whitespace variants. Проверяет pure classification
// predicate без выдачи доступа к namespace.
func TestTemplateVariableSourceKindIsClosed(t *testing.T) {
	for _, source := range []string{"", "AGENT", "AUTOMATION", "GATE", "INPUT", "ORGANIZATION", "PROJECT", "RUN", "RUNTIME", "SESSION", "USER", "WORKFLOW"} {
		if !validTemplateVariableSourceKind(source) {
			t.Fatalf("known source rejected: %q", source)
		}
	}
	for _, source := range []string{"agent", "SYSTEM", "UNKNOWN", " PROJECT"} {
		if validTemplateVariableSourceKind(source) {
			t.Fatalf("unknown source accepted: %q", source)
		}
	}
}

// TestTemplateVariableFilterMatchesSearchAndSourceTogether checks name/description search and
// exact source filtering combine conjunctively, including Cyrillic description matching. It tests
// one synthetic variable rather than performing a database search or expanding visibility.
// TestTemplateVariableFilterMatchesSearchAndSourceTogether проверяет совместное применение
// name/description search и exact source filter, включая Cyrillic description matching. Проверяет
// одну synthetic variable без DB search и расширения visibility.
func TestTemplateVariableFilterMatchesSearchAndSourceTogether(t *testing.T) {
	item := entity.TemplateVariable{Name: "workflow.name", Description: "Название процесса", Source: "WORKFLOW"}
	for _, test := range []struct {
		query, source string
		want          bool
	}{
		{"", "", true},
		{"process", "", false},
		{"процесса", "", true},
		{"workflow", "WORKFLOW", true},
		{"workflow", "PROJECT", false},
	} {
		if got := templateVariableMatchesFilter(item, test.query, test.source); got != test.want {
			t.Fatalf("filter query=%q source=%q: got %t want %t", test.query, test.source, got, test.want)
		}
	}
}

// TestTemplateVariableAvailabilityUsesMaterializedValues checks availability derives from actual
// flat/structured snapshot values, including empty file arrays/count0, while absent
// project/workflow/environment refs stay unavailable. It verifies availability reasons for every
// catalog entry without assigning authority from a variable name.
// TestTemplateVariableAvailabilityUsesMaterializedValues проверяет вывод availability из
// фактических flat/structured snapshot values, включая empty file arrays/count0, при недоступности
// absent project/workflow/environment refs. Сверяет availability reasons всех catalog entries без
// назначения authority по variable name.
func TestTemplateVariableAvailabilityUsesMaterializedValues(t *testing.T) {
	snapshot := entity.PromptMaterializationSnapshot{RunRef: "run_example", SessionRef: "ses_example",
		Variables:           map[string]string{"agent.ref": "agt_example", "workflow.ref": "", "organization.ref": "org_example"},
		StructuredVariables: map[string]any{"input": map[string]any{"files": []any{}, "files_count": float64(0)}}}
	availability := materializedVariableAvailability(snapshot)
	for _, name := range []string{"run.ref", "session.ref", "agent.ref", "input.files", "input.files_count"} {
		if !availability[name] {
			t.Fatalf("materialized variable disabled: %s", name)
		}
	}
	for _, name := range []string{"project.ref", "workflow.ref", "runtime.environment.ref"} {
		if availability[name] {
			t.Fatalf("absent variable enabled: %s", name)
		}
	}
	for _, variable := range templateVariableCatalog() {
		want := variableNotMaterialized
		if availability[variable.Name] {
			want = variableAvailable
		}
		if got := variableAvailabilityReason(variable, availability, true); got != want {
			t.Fatalf("reason %s: %s", variable.Name, got)
		}
	}
}

// TestTemplateOrganizationNameAvailabilityRequiresMaterializedValue checks organization.name is
// available only when its nonempty value was materialized; organization.ref alone cannot imply it.
// The reason must match present/missing/empty cases, without querying organization metadata or
// changing a snapshot.
// TestTemplateOrganizationNameAvailabilityRequiresMaterializedValue проверяет availability
// organization.name лишь при materialized nonempty value; одного organization.ref недостаточно.
// Reason совпадает с present/missing/empty cases без чтения organization metadata и изменения
// snapshot.
func TestTemplateOrganizationNameAvailabilityRequiresMaterializedValue(t *testing.T) {
	for _, test := range []struct {
		name      string
		variables map[string]string
		available bool
	}{
		{"present", map[string]string{"organization.ref": "org_example", "organization.name": "Организация & проект"}, true},
		{"missing", map[string]string{"organization.ref": "org_example"}, false},
		{"empty", map[string]string{"organization.ref": "org_example", "organization.name": ""}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			available := materializedVariableAvailability(entity.PromptMaterializationSnapshot{Variables: test.variables})
			if available["organization.name"] != test.available {
				t.Fatal("organization name availability does not match the materialized value")
			}
			want := variableNotMaterialized
			if test.available {
				want = variableAvailable
			}
			if got := variableAvailabilityReason(entity.TemplateVariable{Name: "organization.name"}, available, true); got != want {
				t.Fatal("organization name availability reason is inconsistent")
			}
		})
	}
}
