package callback

import (
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// assistantPDLCReadFixture models the owner's paired editable/private projections after a
// four-role PDLC update, without manufacturing grants, receipts or accepted business results.
// assistantPDLCReadFixture воспроизводит парные редактируемую и внутреннюю проекции владельца
// после настройки четырёх ролей PDLC без создания grants, receipts и принятых результатов.
func assistantPDLCReadFixture(t *testing.T) (runtimecontract.RunnerInput, map[string]any, *controlplanev1.AssistantConfigurationCatalogResponse) {
	t.Helper()
	input, arguments, response := assistantWorkflowConfigurationFixture(t)
	requirements := [][]map[string]any{
		{
			{"key": "spec", "kind": "REPOSITORY_FILE", "repository": "codex-k8s/test-repo", "targetRef": "3", "branch": "feature/journal", "fileName": "docs/requirements.md"},
			{"key": "comment", "kind": "PR_COMMENT", "repository": "codex-k8s/test-repo", "targetRef": "3"},
		},
		{
			{"key": "source", "kind": "REPOSITORY_FILE", "repository": "codex-k8s/test-repo", "targetRef": "3", "branch": "feature/journal", "fileName": "src/server.js"},
			{"key": "pr", "kind": "PULL_REQUEST", "repository": "codex-k8s/test-repo", "targetRef": "3"},
		},
		{{"key": "review", "kind": "REVIEW", "subjectStepKey": "step-002"}},
		{
			{"key": "qa", "kind": "QA", "subjectStepKey": "step-002", "evidenceKey": "report"},
			{"key": "report", "kind": "ARTIFACT", "fileName": "qa-report.json", "mediaType": "application/json"},
		},
	}
	rewriteAssistantWorkflowConfigurationFixture(t, response, func(snapshot map[string]any) {
		draft := snapshot["draft"].(map[string]any)
		snapshot["executionMode"], draft["ExecutionMode"] = "SERVER_DAG_V1", "SERVER_DAG_V1"
		snapshot["resultEvidenceProfile"], draft["ResultEvidenceProfile"] = "GITHUB_PDLC_V1", "GITHUB_PDLC_V1"
		snapshot["maxConcurrency"], draft["Concurrency"] = float64(2), float64(2)
		steps, originals := snapshot["steps"].([]any)[:4], draft["Steps"].([]any)[:4]
		for index := range steps {
			step, original := steps[index].(map[string]any), originals[index].(map[string]any)
			step["dependsOn"] = original["DependsOn"]
			if index == 3 {
				step["dependsOn"], original["DependsOn"] = []any{"step-002", "step-003"}, []any{"step-002", "step-003"}
			}
			editable, stored := []any{}, []any{}
			for _, requirement := range requirements[index] {
				upper := map[string]any{"Key": "", "Kind": "", "FileName": "", "MediaType": "", "Repository": "", "SubjectStepKey": "", "SubjectResultKey": "", "Operation": "", "TargetRef": ""}
				for lower, title := range map[string]string{"key": "Key", "kind": "Kind", "fileName": "FileName", "mediaType": "MediaType", "repository": "Repository", "subjectStepKey": "SubjectStepKey", "subjectResultKey": "SubjectResultKey", "operation": "Operation", "targetRef": "TargetRef", "branch": "Branch", "evidenceKey": "EvidenceKey"} {
					if value, exists := requirement[lower]; exists {
						upper[title] = value
					}
				}
				editable, stored = append(editable, requirement), append(stored, upper)
			}
			step["resultRequirements"], original["ResultRequirements"] = editable, stored
		}
		snapshot["steps"], draft["Steps"] = steps, originals
	})
	return input, arguments, response
}

// TestAssistantPDLCConfigurationReadTraversesNativeMCP reconstructs the complete canonical
// owner snapshot through bounded MCP pages, preserving profile, branch and QA evidence keys.
// TestAssistantPDLCConfigurationReadTraversesNativeMCP восстанавливает полный канонический
// снимок владельца через ограниченные страницы MCP с сохранением профиля, ветки и QA evidence keys.
func TestAssistantPDLCConfigurationReadTraversesNativeMCP(t *testing.T) {
	input, arguments, response := assistantPDLCReadFixture(t)
	arguments["assistant_configuration_catalog"].(map[string]any)["maximum_bytes"] = float64(512)
	client := &assistantFreshCatalogMCPClient{assistantDefinitionCatalogClient: &assistantDefinitionCatalogClient{response: &controlplanev1.SearchAssistantResourcesResponse{AssistantConfigurationCatalog: response}}}
	server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
	if pages := readAssistantConfigurationMCP(t, input, arguments, server, "workflow_configuration", response.WorkflowConfiguration.ConfigurationJson); pages < 2 {
		t.Fatal("native configuration read did not exercise continuation pages")
	}
}

// TestAssistantReviewQAConfigurationReadTraversesNativeMCP preserves the earlier native QA
// profile's report selector without accepting the newer repository-file selectors by default.
// TestAssistantReviewQAConfigurationReadTraversesNativeMCP сохраняет selector отчёта прежнего
// native QA профиля без неявного допуска новых selectors файлов репозитория.
func TestAssistantReviewQAConfigurationReadTraversesNativeMCP(t *testing.T) {
	input, arguments, response := assistantPDLCReadFixture(t)
	rewriteAssistantWorkflowConfigurationFixture(t, response, func(snapshot map[string]any) {
		draft := snapshot["draft"].(map[string]any)
		snapshot["resultEvidenceProfile"], draft["ResultEvidenceProfile"] = "GITHUB_REVIEW_QA_V1", "GITHUB_REVIEW_QA_V1"
		steps, originals := snapshot["steps"].([]any), draft["Steps"].([]any)
		first, original := steps[0].(map[string]any), originals[0].(map[string]any)
		artifact := first["resultRequirements"].([]any)[0].(map[string]any)
		stored := original["ResultRequirements"].([]any)[0].(map[string]any)
		artifact["kind"], artifact["fileName"], artifact["mediaType"] = "ARTIFACT", "spec.md", "text/markdown"
		stored["Kind"], stored["FileName"], stored["MediaType"], stored["Repository"], stored["TargetRef"] = "ARTIFACT", "spec.md", "text/markdown", "", ""
		for _, key := range []string{"repository", "targetRef", "branch"} {
			delete(artifact, key)
		}
		delete(stored, "Branch")
		first["resultRequirements"], original["ResultRequirements"] = []any{artifact}, []any{stored}
		implementation, implementationOriginal := steps[1].(map[string]any), originals[1].(map[string]any)
		pull := implementation["resultRequirements"].([]any)[1].(map[string]any)
		pullOriginal := implementationOriginal["ResultRequirements"].([]any)[1].(map[string]any)
		delete(pull, "targetRef")
		pullOriginal["TargetRef"] = ""
		implementation["resultRequirements"], implementationOriginal["ResultRequirements"] = []any{pull}, []any{pullOriginal}
	})
	client := &assistantFreshCatalogMCPClient{assistantDefinitionCatalogClient: &assistantDefinitionCatalogClient{response: &controlplanev1.SearchAssistantResourcesResponse{AssistantConfigurationCatalog: response}}}
	server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
	readAssistantConfigurationMCP(t, input, arguments, server, "workflow_configuration", response.WorkflowConfiguration.ConfigurationJson)
}

// TestAssistantPDLCConfigurationReadKeepsClosedBoundary rejects mismatched profiles and
// selectors, legacy-profile downgrade, nullable fields and caller-supplied evidence coordinates.
// TestAssistantPDLCConfigurationReadKeepsClosedBoundary отвергает несовпадающие профили и
// selectors, подмену прежним профилем, nullable поля и переданные caller координаты доказательств.
func TestAssistantPDLCConfigurationReadKeepsClosedBoundary(t *testing.T) {
	for _, name := range []string{"profile-mismatch", "unknown-profile", "null-profile", "legacy-profile", "older-native-profile", "legacy-mode", "branch-mismatch", "null-branch", "null-branch-both", "qa-key-mismatch", "null-qa-key", "null-qa-key-both", "receipt-in-config"} {
		t.Run(name, func(t *testing.T) {
			input, arguments, response := assistantPDLCReadFixture(t)
			rewriteAssistantWorkflowConfigurationFixture(t, response, func(snapshot map[string]any) {
				draft := snapshot["draft"].(map[string]any)
				step := snapshot["steps"].([]any)[0].(map[string]any)
				file := step["resultRequirements"].([]any)[0].(map[string]any)
				qa := snapshot["steps"].([]any)[3].(map[string]any)["resultRequirements"].([]any)[0].(map[string]any)
				switch name {
				case "profile-mismatch":
					snapshot["resultEvidenceProfile"] = "GITHUB_REVIEW_QA_V1"
				case "unknown-profile":
					snapshot["resultEvidenceProfile"], draft["ResultEvidenceProfile"] = "UNKNOWN", "UNKNOWN"
				case "null-profile":
					snapshot["resultEvidenceProfile"], draft["ResultEvidenceProfile"] = nil, nil
				case "legacy-profile":
					delete(snapshot, "resultEvidenceProfile")
					delete(draft, "ResultEvidenceProfile")
				case "older-native-profile":
					snapshot["resultEvidenceProfile"], draft["ResultEvidenceProfile"] = "GITHUB_REVIEW_QA_V1", "GITHUB_REVIEW_QA_V1"
				case "legacy-mode":
					snapshot["executionMode"], draft["ExecutionMode"] = "MANAGER_DRIVEN", "MANAGER_DRIVEN"
				case "branch-mismatch":
					file["branch"] = "feature/other"
				case "null-branch":
					file["branch"] = nil
				case "null-branch-both":
					file["branch"] = nil
					draft["Steps"].([]any)[0].(map[string]any)["ResultRequirements"].([]any)[0].(map[string]any)["Branch"] = nil
				case "qa-key-mismatch":
					qa["evidenceKey"] = "other_report"
				case "null-qa-key":
					qa["evidenceKey"] = nil
				case "null-qa-key-both":
					qa["evidenceKey"] = nil
					draft["Steps"].([]any)[3].(map[string]any)["ResultRequirements"].([]any)[0].(map[string]any)["EvidenceKey"] = nil
				case "receipt-in-config":
					file["receiptRef"] = "erc_unsupported123"
				}
			})
			request, err := parseAssistantConfigurationCatalog(input, arguments, arguments["assistant_configuration_catalog"])
			if err != nil {
				t.Fatal("prepare legitimate native read", err)
			}
			if value, err := castAssistantConfigurationCatalog(input, request, response); err == nil || value != nil {
				t.Fatal("invalid owner projection crossed closed read boundary")
			}
		})
	}
}
