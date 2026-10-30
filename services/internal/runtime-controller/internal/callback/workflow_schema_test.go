package callback

import (
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

// TestWorkflowMCPPDLCProfile validates the actual published MCP CREATE/UPDATE schemas, including
// native file targets, separate local outputs, old-profile denial and owner-resolved sparse updates.
// TestWorkflowMCPPDLCProfile проверяет actual MCP CREATE/UPDATE schemas с native file targets,
// отдельными local outputs, запретом прежнего профиля и owner-resolved sparse updates.
func TestWorkflowMCPPDLCProfile(t *testing.T) {
	create := resolvedWorkflowSchema(t, workflowInputSchema(opaqueRefSchema(), opaqueRefSchema()))
	update := resolvedWorkflowSchema(t, workflowUpdateInputSchema("wfl_fixture123"))
	fixture := func() map[string]any {
		value := assistantWorkflowSchemaFixture(t)
		value["resultEvidenceProfile"] = "GITHUB_PDLC_V1"
		value["steps"].([]any)[0].(map[string]any)["resultRequirements"] = []any{
			map[string]any{"key": "document", "kind": "REPOSITORY_FILE", "repository": "owner/repo", "targetRef": "3", "branch": "feature/journal", "fileName": "docs/requirements.md"},
			map[string]any{"key": "comment", "kind": "PR_COMMENT", "repository": "owner/repo", "targetRef": "3"},
			map[string]any{"key": "report", "kind": "ARTIFACT", "fileName": "requirements.md", "mediaType": "text/markdown"},
			map[string]any{"key": "pr", "kind": "PULL_REQUEST", "repository": "owner/repo", "targetRef": "3"},
		}
		return value
	}
	if err := create.Validate(fixture()); err != nil {
		t.Fatal("actual MCP create rejected native PDLC targets", err)
	}
	for _, mode := range []string{"missing-profile", "old-profile", "unknown-profile", "null-profile", "missing-branch", "null-branch", "receipt-in-config", "missing-pr", "missing-mode", "parallel-overflow"} {
		t.Run(mode, func(t *testing.T) {
			value := fixture()
			results := value["steps"].([]any)[0].(map[string]any)["resultRequirements"].([]any)
			file := results[0].(map[string]any)
			switch mode {
			case "missing-profile":
				delete(value, "resultEvidenceProfile")
			case "old-profile":
				value["resultEvidenceProfile"] = "GITHUB_REVIEW_QA_V1"
			case "unknown-profile":
				value["resultEvidenceProfile"] = "GITHUB_PDLC_V2"
			case "null-profile":
				value["resultEvidenceProfile"] = nil
			case "missing-branch":
				delete(file, "branch")
			case "null-branch":
				file["branch"] = nil
			case "receipt-in-config":
				file["receiptRef"] = "erc_unsupported0"
			case "missing-pr":
				delete(results[3].(map[string]any), "targetRef")
			case "missing-mode":
				delete(value, "executionMode")
			case "parallel-overflow":
				value["maxConcurrency"] = float64(3)
			}
			if create.Validate(value) == nil {
				t.Fatal("invalid native PDLC configuration crossed MCP schema")
			}
		})
	}
	value := fixture()
	delete(value, "projectRef")
	delete(value, "executionMode")
	delete(value, "resultEvidenceProfile")
	value["workflowRef"] = "wfl_fixture123"
	if err := update.Validate(value); err != nil {
		t.Fatal("sparse update was rejected before owner resolved existing PDLC profile", err)
	}
	for _, profile := range []string{"", "GITHUB_REVIEW_QA_V1"} {
		value["resultEvidenceProfile"] = profile
		if update.Validate(value) == nil {
			t.Fatal("explicit old update profile accepted native file fields")
		}
	}
}

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

// TestWorkflowMCPSchemaLegacyCreationRejectsServerFields checks each forbidden field independently
// for both legacy mode forms while keeping legacy keys and sparse server updates editable.
// TestWorkflowMCPSchemaLegacyCreationRejectsServerFields проверяет каждое запрещённое поле
// для обоих прежних режимов, сохраняя изменение старых ключей и частичное изменение DAG.
func TestWorkflowMCPSchemaLegacyCreationRejectsServerFields(t *testing.T) {
	create := resolvedWorkflowSchema(t, workflowInputSchema(opaqueRefSchema(), opaqueRefSchema()))
	update := resolvedWorkflowSchema(t, workflowUpdateInputSchema("wfl_fixture123"))
	for _, mode := range []string{"", "MANAGER_DRIVEN"} {
		for _, field := range []string{"key", "dependsOn", "resultRequirements"} {
			t.Run(mode+"/"+field, func(t *testing.T) {
				value := assistantWorkflowSchemaFixture(t)
				delete(value, "executionMode")
				if mode != "" {
					value["executionMode"] = mode
				}
				step := value["steps"].([]any)[0].(map[string]any)
				for _, other := range []string{"key", "dependsOn", "resultRequirements"} {
					if other != field {
						delete(step, other)
					}
				}
				if err := create.Validate(value); err == nil {
					t.Fatal("legacy create accepted server-only field", field)
				}
				delete(step, field)
				if err := create.Validate(value); err != nil {
					t.Fatal("legacy create without server fields rejected", err)
				}
				step["key"] = "step_existing"
				value["workflowRef"] = "wfl_fixture123"
				delete(value, "projectRef")
				if err := update.Validate(value); err != nil {
					t.Fatal("legacy update with stable key rejected", err)
				}
			})
		}
	}
	sparse := assistantWorkflowSchemaFixture(t)
	delete(sparse, "executionMode")
	delete(sparse, "projectRef")
	sparse["workflowRef"] = "wfl_fixture123"
	if err := update.Validate(sparse); err != nil {
		t.Fatal("server update without repeated mode rejected before owner readback", err)
	}
}

// TestWorkflowMCPEvidenceProfile validates the shipped compiled CREATE/UPDATE schema, preserving
// optional absence and explicit update clearing while never accepting null or legacy opt-in.
// TestWorkflowMCPEvidenceProfile проверяет поставляемую compiled CREATE/UPDATE schema, сохраняя
// отсутствие поля и явное снятие opt-in при UPDATE без принятия null либо legacy opt-in.
func TestWorkflowMCPEvidenceProfile(t *testing.T) {
	create := resolvedWorkflowSchema(t, workflowInputSchema(opaqueRefSchema(), opaqueRefSchema()))
	update := resolvedWorkflowSchema(t, workflowUpdateInputSchema("wfl_fixture123"))
	fixture := func() map[string]any {
		value := assistantWorkflowSchemaFixture(t)
		value["resultEvidenceProfile"] = "GITHUB_REVIEW_QA_V1"
		step := value["steps"].([]any)[0].(map[string]any)
		step["dependsOn"] = []any{"author"}
		step["resultRequirements"] = []any{
			map[string]any{"key": "report", "kind": "ARTIFACT", "fileName": "qa.json", "mediaType": "application/json"},
			map[string]any{"key": "qa", "kind": "QA", "subjectStepKey": "author", "evidenceKey": "report"},
		}
		return value
	}
	if err := create.Validate(fixture()); err != nil {
		t.Fatal("valid evidence profile schema rejected", err)
	}
	for name, mutate := range map[string]func(map[string]any){
		"missing mode": func(v map[string]any) { delete(v, "executionMode") },
		"null":         func(v map[string]any) { v["resultEvidenceProfile"] = nil },
		"unknown":      func(v map[string]any) { v["resultEvidenceProfile"] = "UNKNOWN" },
		"create clear": func(v map[string]any) { v["resultEvidenceProfile"] = "" },
		"missing evidence key": func(v map[string]any) {
			delete(v["steps"].([]any)[0].(map[string]any)["resultRequirements"].([]any)[1].(map[string]any), "evidenceKey")
		},
		"non QA evidence key": func(v map[string]any) {
			v["steps"].([]any)[0].(map[string]any)["resultRequirements"].([]any)[0].(map[string]any)["evidenceKey"] = "report"
		},
	} {
		t.Run(name, func(t *testing.T) {
			value := fixture()
			mutate(value)
			if create.Validate(value) == nil {
				t.Fatal("invalid evidence profile accepted by MCP schema")
			}
		})
	}
	for _, profile := range []any{nil, "UNKNOWN"} {
		if update.Validate(map[string]any{"workflowRef": "wfl_fixture123", "resultEvidenceProfile": profile}) == nil {
			t.Fatal("invalid optional update evidence profile accepted")
		}
	}
	for _, value := range []map[string]any{
		{"workflowRef": "wfl_fixture123", "name": "Сохранить прежний профиль"},
		{"workflowRef": "wfl_fixture123", "resultEvidenceProfile": ""},
		{"workflowRef": "wfl_fixture123", "resultEvidenceProfile": "GITHUB_REVIEW_QA_V1"},
	} {
		if err := update.Validate(value); err != nil {
			t.Fatal("sparse evidence profile update rejected", err)
		}
	}
}
