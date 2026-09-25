package platform

import (
	"encoding/json"
	"errors"
	"testing"

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
	for _, key := range []string{"specification", "policy", "values", "tools"} {
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
