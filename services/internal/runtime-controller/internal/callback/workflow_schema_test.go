package callback

import (
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

// resolvedWorkflowSchema compiles the actual MCP schema without network references; fixtures
// exercise JSON Schema validation rather than merely checking selected property names.
// resolvedWorkflowSchema компилирует настоящую MCP-схему без сетевых ссылок; оснастка проверяет
// валидацию JSON Schema, а не только наличие отдельных имён полей.
func resolvedWorkflowSchema(t *testing.T, value map[string]any) *jsonschema.Resolved {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// assistantWorkflowSchemaFixture creates a fresh configuration proposal, without accepted
// evidence, granted tools or external effects.
// assistantWorkflowSchemaFixture создаёт новую конфигурацию без принятых доказательств,
// выданных tools или внешних действий.
func assistantWorkflowSchemaFixture(t *testing.T) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(`{"projectRef":"prj_fixture123","name":"Журнал обращений","purpose":"Проверить требования","coordinatorAgentRef":"agt_manager123","executionMode":"SERVER_DAG_V1","maxConcurrency":2,"steps":[{"key":"qa","name":"Проверка","purpose":"Подготовить заключение","agentRef":"agt_qa_fixture","parallel":false,"parallelGroup":0,"timeoutSeconds":300,"expectedResult":"Заключение","humanGate":true,"approvalTimeoutSeconds":90,"gateDecisions":["APPROVE"],"requiredCapabilityKeys":[],"dependsOn":[],"resultRequirements":[{"key":"report","kind":"ARTIFACT","fileName":"report.md","mediaType":"text/markdown"}]}]}`), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

// TestWorkflowMCPSchemaBoundsAndApprovalPolicy checks create and sparse update schemas against
// the bounded server profile, optional approval timeout and unchanged larger legacy graphs.
// TestWorkflowMCPSchemaBoundsAndApprovalPolicy проверяет создание и частичное изменение:
// серверные пределы, необязательный срок согласования и прежние большие графы.
func TestWorkflowMCPSchemaBoundsAndApprovalPolicy(t *testing.T) {
	create := resolvedWorkflowSchema(t, workflowInputSchema(opaqueRefSchema(), opaqueRefSchema()))
	update := resolvedWorkflowSchema(t, workflowUpdateInputSchema("wfl_fixture123"))
	if err := create.Validate(assistantWorkflowSchemaFixture(t)); err != nil {
		t.Fatal("valid server create rejected", err)
	}
	for _, requirement := range []map[string]any{
		{"key": "file", "kind": "ARTIFACT", "fileName": "spec.md", "mediaType": "text/markdown"},
		{"key": "pr", "kind": "PULL_REQUEST", "repository": "codex-k8s/test-repo"},
		{"key": "review", "kind": "REVIEW", "subjectStepKey": "code"},
		{"key": "qa", "kind": "QA", "subjectStepKey": "code"},
		{"key": "merge", "kind": "INTEGRATION_RECEIPT", "subjectStepKey": "code", "repository": "codex-k8s/test-repo", "operation": "github.pull_request.merge"},
		{"key": "deploy", "kind": "DEPLOYMENT", "subjectStepKey": "code", "repository": "codex-k8s/test-repo", "operation": "ssh.compose.deploy", "targetRef": "ssh_fixture123"},
	} {
		value := assistantWorkflowSchemaFixture(t)
		value["steps"].([]any)[0].(map[string]any)["resultRequirements"] = []any{requirement}
		if err := create.Validate(value); err != nil {
			t.Fatal("closed result kind rejected", requirement["kind"], err)
		}
	}
	for name, mutate := range map[string]func(map[string]any, map[string]any){
		"unknown mode":      func(v, _ map[string]any) { v["executionMode"] = "AUTOMATIC" },
		"concurrency three": func(v, _ map[string]any) { v["maxConcurrency"] = float64(3) },
		"thirteen steps": func(v, s map[string]any) {
			a := []any{}
			for range 13 {
				a = append(a, s)
			}
			v["steps"] = a
		},
		"missing key":     func(_, s map[string]any) { delete(s, "key") },
		"missing deps":    func(_, s map[string]any) { delete(s, "dependsOn") },
		"missing results": func(_, s map[string]any) { delete(s, "resultRequirements") },
		"empty results":   func(_, s map[string]any) { s["resultRequirements"] = []any{} },
		"five results": func(_, s map[string]any) {
			r := s["resultRequirements"].([]any)[0]
			s["resultRequirements"] = []any{r, r, r, r, r}
		},
		"cross-kind field": func(_, s map[string]any) {
			s["resultRequirements"].([]any)[0].(map[string]any)["repository"] = "codex-k8s/test-repo"
		},
		"missing mime":     func(_, s map[string]any) { delete(s["resultRequirements"].([]any)[0].(map[string]any), "mediaType") },
		"invalid key":      func(_, s map[string]any) { s["key"] = "qa/other" },
		"owner field":      func(_, s map[string]any) { s["resultRequirements"].([]any)[0].(map[string]any)["owner"] = "actor" },
		"ttl zero":         func(_, s map[string]any) { s["approvalTimeoutSeconds"] = float64(0) },
		"ttl overflow":     func(_, s map[string]any) { s["approvalTimeoutSeconds"] = float64(604801) },
		"ttl without gate": func(_, s map[string]any) { s["humanGate"] = false },
	} {
		t.Run(name, func(t *testing.T) {
			value := assistantWorkflowSchemaFixture(t)
			mutate(value, value["steps"].([]any)[0].(map[string]any))
			if err := create.Validate(value); err == nil {
				t.Fatal("invalid create accepted")
			}
			value["workflowRef"] = "wfl_fixture123"
			delete(value, "projectRef")
			if err := update.Validate(value); err == nil {
				t.Fatal("invalid update accepted")
			}
		})
	}
	legacy := assistantWorkflowSchemaFixture(t)
	delete(legacy, "executionMode")
	legacy["maxConcurrency"] = float64(4)
	step := legacy["steps"].([]any)[0].(map[string]any)
	for _, key := range []string{"key", "dependsOn", "resultRequirements", "approvalTimeoutSeconds"} {
		delete(step, key)
	}
	for len(legacy["steps"].([]any)) < 13 {
		legacy["steps"] = append(legacy["steps"].([]any), step)
	}
	if err := create.Validate(legacy); err != nil {
		t.Fatal("legacy graph acquired first-profile limits", err)
	}
	if err := update.Validate(map[string]any{"workflowRef": "wfl_fixture123", "executionMode": "SERVER_DAG_V1"}); err != nil {
		t.Fatal("mode-only proposal unavailable before owner validation", err)
	}
}
