package platform

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestAssistantEnvironmentRevisionPreservesProtectedSpecification(t *testing.T) {
	t.Parallel()
	specification := entity.RuntimeEnvironmentDraftSpecification{
		Name: "Original", Description: "Original description", ImageArtifactRef: "imgart_original",
		Values:         []entity.RuntimeEnvironmentValue{{Name: "MODE", Value: "safe"}},
		SecretBindings: []entity.RuntimeSecretBinding{{Name: "TOKEN", SecretRef: "sec_exact", Revision: 3}},
		Tools:          []entity.RuntimeEnvironmentTool{{Name: "git", Command: "git"}},
	}
	raw, err := json.Marshal(specification)
	if err != nil {
		t.Fatal(err)
	}
	var safe map[string]any
	if err := json.Unmarshal(raw, &safe); err != nil {
		t.Fatal(err)
	}
	before := map[string]any{
		"environmentRef": "renv_exact", "projectRef": "prj_exact", "name": "Original",
		"description": "Original description", "imageArtifactRef": "imgart_original",
		"versionRef": "renvv_exact", "versionDigest": "digest-exact", "specification": safe,
	}
	if !assistantEnvironmentSnapshotIdentityMatches(before, cloneAssistantFields(before)) {
		t.Fatal("exact environment revision identity did not match")
	}
	changedDigest := cloneAssistantFields(before)
	changedDigest["versionDigest"] = "digest-other"
	if assistantEnvironmentSnapshotIdentityMatches(before, changedDigest) {
		t.Fatal("changed environment revision digest matched")
	}
	proposed := entity.AssistantPlanOperation{
		Type: "PREPARE_RUNTIME_ENVIRONMENT_REVISION", Key: "environment-revision",
		Title: "Prepare environment revision", Summary: "Prepare exact draft",
		Parameters: map[string]any{"environmentRef": "renv_exact", "name": "Updated"},
	}
	if !assistantOperationMatchesContext("ENVIRONMENT", "renv_exact", proposed) ||
		assistantOperationMatchesContext("ENVIRONMENT", "renv_other", proposed) ||
		assistantOperationMatchesContext("PROJECT", "renv_exact", proposed) {
		t.Fatal("environment revision accepted a different context")
	}
	hydrated, err := hydrateAssistantEnvironmentFields(before, 7, proposed)
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := normalizeAssistantOperation(hydrated)
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := assistantOperationCommand(normalized)
	if err != nil || mapped.Kind != command.CreateRuntimeEnvironmentDraft {
		t.Fatalf("environment revision command invalid: %#v %v", mapped, err)
	}
	payload := mapped.Payload.(command.RuntimeEnvironmentDraftInput)
	if payload.EnvironmentRef != "renv_exact" || payload.ProjectRef != "prj_exact" ||
		payload.ExpectedEnvironmentVersion != 7 || payload.Specification.Name != "Updated" ||
		payload.Specification.Description != "Original description" ||
		payload.Specification.ImageArtifactRef != "imgart_original" ||
		len(payload.Specification.Values) != 1 || payload.Specification.Values[0].Value != "safe" ||
		len(payload.Specification.SecretBindings) != 1 || payload.Specification.SecretBindings[0].SecretRef != "sec_exact" ||
		len(payload.Specification.Tools) != 1 || payload.Specification.Tools[0].Name != "git" {
		t.Fatalf("environment revision lost protected specification: %#v", payload)
	}
	edited := normalized
	edited.Parameters = cloneAssistantFields(normalized.Parameters)
	edited.Parameters["description"] = "Updated description"
	result, err := rehydrateEditedAssistantEnvironment(normalized, edited)
	if err != nil || assistantString(result.After, "description") != "Updated description" ||
		result.Target.Ref != "renv_exact" || *result.ExpectedVersion != 7 {
		t.Fatalf("environment edit lost authoritative envelope: %#v %v", result, err)
	}
	valuesEdit := normalized
	valuesEdit.Parameters = cloneAssistantFields(normalized.Parameters)
	valuesEdit.Parameters["publicValues"] = []any{map[string]any{"name": "MODE", "value": "updated"}}
	valuesEdit.Parameters["secretBindings"] = []any{map[string]any{"name": "TOKEN", "secretRef": "sec_exact", "revision": float64(3)}}
	valuesResult, err := rehydrateEditedAssistantEnvironment(normalized, valuesEdit)
	if err != nil {
		t.Fatalf("environment value edit refused: %v", err)
	}
	valuesResult, err = normalizeAssistantOperation(valuesResult)
	if err != nil {
		t.Fatalf("environment value edit could not be normalized: %v", err)
	}
	valuesCommand, err := assistantOperationCommand(valuesResult)
	if err != nil {
		t.Fatalf("environment value command refused: %v", err)
	}
	updated := valuesCommand.Payload.(command.RuntimeEnvironmentDraftInput).Specification
	if len(updated.Values) != 1 || updated.Values[0].Value != "updated" ||
		len(updated.SecretBindings) != 1 || updated.SecretBindings[0].Revision != 3 ||
		len(updated.Tools) != 1 || updated.Tools[0].Name != "git" {
		t.Fatalf("environment revision did not preserve immutable fields: %#v", updated)
	}
	patchProposal := proposed
	patchProposal.Parameters = map[string]any{
		"environmentRef": "renv_exact",
		"publicValueUpdates": []any{
			map[string]any{"name": "MODE", "value": "patched"},
			map[string]any{"name": "ASSISTANT_REVISION_TEST", "value": "draft"},
		},
	}
	patchResult, err := hydrateAssistantEnvironmentFields(before, 7, patchProposal)
	if err != nil {
		t.Fatalf("environment sparse value patch refused: %v", err)
	}
	if patchResult.Parameters["publicValueUpdates"] != nil || patchResult.Parameters["publicValueRemovals"] != nil {
		t.Fatalf("environment sparse patch escaped hydration: %#v", patchResult.Parameters)
	}
	patchValues, valid := assistantEnvironmentPublicValues(patchResult.Parameters)
	if !valid || len(patchValues) != 2 || patchValues[0].Name != "MODE" || patchValues[0].Value != "patched" ||
		patchValues[1].Name != "ASSISTANT_REVISION_TEST" || patchValues[1].Value != "draft" {
		t.Fatalf("environment sparse patch was not materialized: %#v", patchResult.Parameters)
	}
	removeProposal := proposed
	removeProposal.Parameters = map[string]any{
		"environmentRef": "renv_exact", "publicValueRemovals": []any{"MODE"},
	}
	removeResult, err := hydrateAssistantEnvironmentFields(before, 7, removeProposal)
	if err != nil {
		t.Fatalf("environment sparse value removal refused: %v", err)
	}
	removedValues, valid := assistantEnvironmentPublicValues(removeResult.Parameters)
	if !valid || len(removedValues) != 0 {
		t.Fatalf("environment sparse removal was not materialized: %#v", removeResult.Parameters)
	}
	toolsEdit := normalized
	toolsEdit.Parameters = cloneAssistantFields(normalized.Parameters)
	toolsEdit.Parameters["tools"] = []any{map[string]any{"name": "Git", "command": "git", "description": "Manage source files", "usageHint": "Use the verified Git command"}}
	toolsResult, err := rehydrateEditedAssistantEnvironment(normalized, toolsEdit)
	if err != nil {
		t.Fatalf("environment tools edit refused: %v", err)
	}
	toolsResult, err = normalizeAssistantOperation(toolsResult)
	if err != nil {
		t.Fatalf("environment tools edit could not be normalized: %v", err)
	}
	toolsCommand, err := assistantOperationCommand(toolsResult)
	if err != nil {
		t.Fatalf("environment tools command refused: %v", err)
	}
	tools := toolsCommand.Payload.(command.RuntimeEnvironmentDraftInput).Specification.Tools
	if len(tools) != 1 || tools[0].Name != "Git" || tools[0].Command != "git" || tools[0].UsageHint != "Use the verified Git command" {
		t.Fatalf("environment tools edit lost fields: %#v", tools)
	}
	policyEdit := normalized
	policyEdit.Parameters = cloneAssistantFields(normalized.Parameters)
	policyEdit.Parameters["policy"] = assistantTestEnvironmentPolicy()
	policyResult, err := rehydrateEditedAssistantEnvironment(normalized, policyEdit)
	if err != nil {
		t.Fatalf("environment policy edit refused: %v", err)
	}
	policyResult, err = normalizeAssistantOperation(policyResult)
	if err != nil {
		t.Fatalf("environment policy edit could not be normalized: %v", err)
	}
	policyCommand, err := assistantOperationCommand(policyResult)
	if err != nil {
		t.Fatalf("environment policy command refused: %v", err)
	}
	policy := policyCommand.Payload.(command.RuntimeEnvironmentDraftInput).Specification.Policy
	if policy.Resources.CPURequestMilli != 1000 || policy.KubernetesAccess.Kind != runtimecontract.RuntimeKubernetesAccessNone ||
		len(policy.Network.Egress) != 4 {
		t.Fatalf("environment policy edit lost admission limits: %#v", policy)
	}
	for _, key := range []string{"specification", "values"} {
		forged := edited
		forged.Parameters = cloneAssistantFields(normalized.Parameters)
		forged.Parameters[key] = "forged"
		if _, err := rehydrateEditedAssistantEnvironment(normalized, forged); !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("environment field %s was mutable: %v", key, err)
		}
	}
	for _, invalid := range []map[string]any{
		{"publicValues": []any{map[string]any{"name": "API_TOKEN", "value": "forbidden"}}},
		{"publicValues": []any{map[string]any{"name": "TOKEN", "value": "collision"}}},
		{"secretBindings": []any{map[string]any{"name": "TOKEN", "secretRef": "sec_other", "secretValue": "forged"}}},
		{"tools": "forged"},
		{"tools": []any{map[string]any{"name": "Unsafe", "command": "sh;rm", "description": "Unverified"}}},
		{"tools": []any{map[string]any{"name": "Git", "command": "git", "description": "First"}, map[string]any{"name": "Git again", "command": "git", "description": "Second"}}},
		{"policy": map[string]any{"networkDestinations": []any{"ANY"}}},
	} {
		forged := edited
		forged.Parameters = cloneAssistantFields(normalized.Parameters)
		for key, value := range invalid {
			forged.Parameters[key] = value
		}
		if _, err := rehydrateEditedAssistantEnvironment(normalized, forged); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("invalid environment fields accepted: %v", err)
		}
	}
}

func TestSystemAssistantEnvironmentRevisionCreatesPinnedOrganizationDraft(t *testing.T) {
	t.Parallel()
	specification := entity.RuntimeEnvironmentDraftSpecification{
		Name: "Kodex", Description: "System assistant environment",
		Values: []entity.RuntimeEnvironmentValue{{Name: "MODE", Value: "safe"}},
		Policy: runtimecontract.DefaultRuntimeEnvironmentPolicy(),
	}
	raw, err := json.Marshal(specification)
	if err != nil {
		t.Fatal(err)
	}
	var safe map[string]any
	if err := json.Unmarshal(raw, &safe); err != nil {
		t.Fatal(err)
	}
	before := map[string]any{
		"environmentRef": "renv_system", "projectRef": "", "systemAssistantRef": "agt_system",
		"name": "Kodex", "description": "System assistant environment", "imageArtifactRef": "",
		"versionRef": "renvv_system", "versionDigest": "digest-system", "specification": safe,
		"policyInput": assistantEnvironmentPolicyInput(specification.Policy),
	}
	proposal := entity.AssistantPlanOperation{
		Type: "PREPARE_RUNTIME_ENVIRONMENT_REVISION", Key: "system-environment", Title: "Настроить сеть Kodex",
		Summary: "Разрешить доступ к документации", Parameters: map[string]any{
			"environmentRef": "renv_system", "systemAssistantRef": "agt_system",
			"publicValues": []any{map[string]any{"name": "MODE", "value": "updated"}},
		},
	}
	if !assistantOperationMatchesContext("PROJECT", "prj_current", proposal) {
		t.Fatal("system assistant environment proposal was restricted to the current page entity")
	}
	hydrated, err := hydrateAssistantEnvironmentFields(before, 9, proposal)
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := normalizeAssistantOperation(hydrated)
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := assistantOperationCommand(normalized)
	if err != nil || mapped.Kind != command.CreateOrganizationRuntimeEnvironmentDraft {
		t.Fatalf("system assistant environment command is not an exact organization draft: %#v %v", mapped, err)
	}
	payload := mapped.Payload.(command.RuntimeEnvironmentDraftInput)
	if payload.ScopeKind != "ORGANIZATION" || payload.EnvironmentRef != "renv_system" || payload.ProjectRef != "" || payload.ExpectedEnvironmentVersion != 9 ||
		payload.Specification.Name != "Kodex" || len(payload.Specification.Values) != 1 || payload.Specification.Values[0].Value != "updated" {
		t.Fatalf("system assistant environment draft escaped its protected boundary: %#v", payload)
	}
}

func assistantTestEnvironmentPolicy() map[string]any {
	return map[string]any{
		"resources": map[string]any{
			"cpuRequestMilli": float64(1000), "cpuLimitMilli": float64(2000),
			"memoryRequestMib": float64(1024), "memoryLimitMib": float64(2048),
			"ephemeralStorageRequestMib": float64(512), "ephemeralStorageLimitMib": float64(1024),
		},
		"volumes": []any{}, "networkDestinations": []any{"DNS", "PROVIDER_PROXY", "RUNTIME_CALLBACK"},
		"kubernetesAccess": "NONE",
	}
}

func TestAssistantEnvironmentHTTPMethodsRoundTrip(t *testing.T) {
	t.Parallel()
	jsonFields := func(input map[string]any) map[string]any {
		t.Helper()
		raw, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]any
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	input := assistantTestEnvironmentPolicy()
	input["webAccess"] = map[string]any{"mode": "ALLOWLIST_FULL", "rules": []any{
		map[string]any{"domainPattern": "docs.example.test", "protocol": "HTTPS", "port": float64(443), "httpMethods": []any{"POST", "GET"}},
	}}
	policy, valid := assistantEnvironmentPolicy(map[string]any{"policy": input})
	if !valid || !reflect.DeepEqual(policy.Network.WebAccess.Rules[0].HTTPMethods, []string{"GET", "POST"}) {
		t.Fatal("canonical HTTP method set was not preserved")
	}
	specification := entity.RuntimeEnvironmentDraftSpecification{Name: "Original", Policy: policy}
	raw, err := json.Marshal(specification)
	if err != nil {
		t.Fatal(err)
	}
	var safe map[string]any
	if err := json.Unmarshal(raw, &safe); err != nil {
		t.Fatal(err)
	}
	before := map[string]any{"environmentRef": "renv_exact", "projectRef": "prj_exact", "name": "Original", "description": "",
		"imageArtifactRef": "", "versionRef": "renvv_exact", "versionDigest": "digest-exact", "specification": safe,
		"policyInput": assistantEnvironmentPolicyInput(policy)}
	operation, err := hydrateAssistantEnvironmentFields(before, 7, entity.AssistantPlanOperation{Type: "PREPARE_RUNTIME_ENVIRONMENT_REVISION",
		Key: "methods", Title: "Сохранить методы", Summary: "Точный набор методов", Parameters: map[string]any{"environmentRef": "renv_exact", "name": "Updated", "policy": input}})
	if err != nil {
		t.Fatal(err)
	}
	operation, err = normalizeAssistantOperation(operation)
	if err != nil {
		t.Fatal(err)
	}
	edited := operation
	edited.Parameters = cloneAssistantFields(operation.Parameters)
	edited.Parameters["description"] = "Edited"
	operation, err = rehydrateEditedAssistantEnvironment(operation, edited)
	if err != nil {
		t.Fatal(err)
	}
	operation, err = normalizeAssistantOperation(operation)
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := assistantOperationCommand(operation)
	if err != nil {
		t.Fatal(err)
	}
	methods := mapped.Payload.(command.RuntimeEnvironmentDraftInput).Specification.Policy.Network.WebAccess.Rules[0].HTTPMethods
	if !reflect.DeepEqual(methods, []string{"GET", "POST"}) {
		t.Fatal("prepare/edit/apply changed the HTTP method set")
	}
	roundTrip := jsonFields(map[string]any{"policy": assistantEnvironmentPolicyInput(policy)})
	decoded, valid := assistantEnvironmentPolicy(roundTrip)
	if !valid || !reflect.DeepEqual(decoded, policy) {
		t.Fatal("editable policy round trip lost HTTP methods")
	}
	systemBefore := cloneAssistantFields(before)
	systemBefore["projectRef"], systemBefore["systemAssistantRef"] = "", "agt_system"
	systemOperation, err := hydrateAssistantEnvironmentFields(systemBefore, 7, entity.AssistantPlanOperation{Type: "PREPARE_RUNTIME_ENVIRONMENT_REVISION",
		Key: "system-methods", Title: "Сохранить методы Kodex", Summary: "Точный набор методов", Parameters: map[string]any{
			"environmentRef": "renv_exact", "systemAssistantRef": "agt_system", "name": "Updated", "policy": input}})
	if err != nil {
		t.Fatal(err)
	}
	systemOperation, err = normalizeAssistantOperation(systemOperation)
	if err != nil {
		t.Fatal(err)
	}
	systemMapped, err := assistantOperationCommand(systemOperation)
	if err != nil || systemMapped.Kind != command.CreateOrganizationRuntimeEnvironmentDraft ||
		!reflect.DeepEqual(systemMapped.Payload.(command.RuntimeEnvironmentDraftInput).Specification.Policy.Network.WebAccess.Rules[0].HTTPMethods, []string{"GET", "POST"}) {
		t.Fatalf("system assistant draft lost canonical HTTP methods: %v", err)
	}
	for _, test := range []struct {
		name   string
		change func(map[string]any, map[string]any)
	}{
		{"missing", func(_ map[string]any, rule map[string]any) { delete(rule, "httpMethods") }},
		{"empty", func(_ map[string]any, rule map[string]any) { rule["httpMethods"] = []any{} }},
		{"duplicate", func(_ map[string]any, rule map[string]any) { rule["httpMethods"] = []any{"GET", "GET"} }},
		{"unknown", func(_ map[string]any, rule map[string]any) { rule["httpMethods"] = []any{"TRACE"} }},
		{"lowercase", func(_ map[string]any, rule map[string]any) { rule["httpMethods"] = []any{"get"} }},
		{"type", func(_ map[string]any, rule map[string]any) { rule["httpMethods"] = []any{true} }},
		{"nested-unknown", func(_ map[string]any, rule map[string]any) { rule["extra"] = true }},
		{"read-only-post", func(web map[string]any, _ map[string]any) { web["mode"] = "ALLOWLIST_READ_ONLY" }},
		{"none-rules", func(web map[string]any, _ map[string]any) { web["mode"] = "NONE" }},
		{"full-public-rules", func(web map[string]any, _ map[string]any) { web["mode"] = "FULL_PUBLIC" }},
		{"wrong-protocol", func(_ map[string]any, rule map[string]any) { rule["protocol"] = "HTTP" }},
		{"wrong-port", func(_ map[string]any, rule map[string]any) { rule["port"] = float64(8443) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := jsonFields(input)
			web := candidate["webAccess"].(map[string]any)
			rule := web["rules"].([]any)[0].(map[string]any)
			test.change(web, rule)
			if _, valid := assistantEnvironmentPolicy(map[string]any{"policy": candidate}); valid {
				t.Fatal("invalid HTTP access rule was accepted")
			}
		})
	}
}
