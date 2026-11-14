package platform

import (
	"encoding/json"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// workflowGitHubScopeInput resolves only the exact repository locator inside a sealed invocation scope and removes its transport fields for historical semantic parsers. Provider authority remains in the producing invocation and owner receipt.
// workflowGitHubScopeInput разрешает только точный locator репозитория внутри закреплённой области invocation и удаляет transport-поля для прежних semantic parsers. Полномочия провайдера остаются в producing invocation и owner receipt.
func workflowGitHubScopeInput(rawScope, rawInput []byte) ([]byte, []byte, bool) {
	var values map[string]string
	if json.Unmarshal(rawScope, &values) != nil {
		return nil, nil, false
	}
	if _, scoped := values["scope_kind"]; !scoped {
		return rawScope, rawInput, true
	}
	scope, err := integrationpackage.ParseGitHubResourceScope(values)
	if err != nil || len(values) != 1 && (scope.Kind != "SELECTED" || len(values) != 2) {
		return nil, nil, false
	}
	var input map[string]json.RawMessage
	if json.Unmarshal(rawInput, &input) != nil {
		return nil, nil, false
	}
	var id int64
	var fullName string
	if json.Unmarshal(input["repository_id"], &id) != nil || json.Unmarshal(input["repository_full_name"], &fullName) != nil || !scope.AllowsRepository(id, fullName) {
		return nil, nil, false
	}
	delete(input, "repository_id")
	delete(input, "repository_full_name")
	parts := strings.Split(fullName, "/")
	scopeJSON, _ := json.Marshal(map[string]string{"owner": parts[0], "repository": parts[1]})
	inputJSON, _ := json.Marshal(input)
	return scopeJSON, inputJSON, true
}

// workflowGitHubProof projects a validated sealed GitHub scope into the historical evidence parser without modifying receipt bytes or their digest.
// workflowGitHubProof проецирует проверенную закреплённую область GitHub в прежний evidence parser без изменения receipt bytes либо их digest.
func workflowGitHubProof(proof workflowIntegrationResult) (workflowIntegrationResult, bool) {
	scope, input, valid := workflowGitHubScopeInput(proof.Scope, proof.Input)
	if valid {
		proof.Scope = scope
		proof.Input = input
	}
	return proof, valid
}

// workflowGitHubFileInput validates optional repository locators and binds them into the semantic scope digest before parsing file intent.
// workflowGitHubFileInput проверяет optional repository locators и закрепляет их в semantic scope digest перед разбором file intent.
func workflowGitHubFileInput(raw []byte, scopeDigest string) ([]byte, string, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, "", false
	}
	_, hasID := fields["repository_id"]
	_, hasName := fields["repository_full_name"]
	if !hasID && !hasName {
		return raw, scopeDigest, true
	}
	var id int64
	var name string
	if !hasID || !hasName || json.Unmarshal(fields["repository_id"], &id) != nil || json.Unmarshal(fields["repository_full_name"], &name) != nil || !(integrationpackage.GitHubResourceScope{Kind: "ALL_ACCESSIBLE"}).AllowsRepository(id, name) {
		return nil, "", false
	}
	scopeDigest = workflowResultSHA([]byte("kodex.workflow-github-repository.v1\x00" + scopeDigest + "\x00" + string(fields["repository_id"]) + "\x00" + name))
	delete(fields, "repository_id")
	delete(fields, "repository_full_name")
	result, _ := json.Marshal(fields)
	return result, scopeDigest, true
}
