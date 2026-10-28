package workflow

import (
	"encoding/json"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// CaptureProgressSchema distinguishes a technical capture request from a final result declaration.
// CaptureProgressSchema отличает технический запрос capture от финальной декларации результата.
const CaptureProgressSchema = "kodex.workflow-capture-progress.v1"

// CaptureProgressPhase allows only the explicit intermediate capture phase, never inferred failure.
// CaptureProgressPhase разрешает только явную промежуточную фазу capture, не выводимую из ошибки.
const CaptureProgressPhase = "CAPTURE_PROGRESS"

// CaptureProgressCause requests a fresh catalog for already prepared files without accepting them.
// CaptureProgressCause запрашивает свежий каталог подготовленных файлов, не принимая их результат.
const CaptureProgressCause = "ARTIFACTS_FOR_CONTINUATION"

// DecodeCaptureProgress validates a JSON-only request for one technical continuation in SERVER_DAG_V1.
// Keys select a nonempty subset of published ARTIFACT requirements, never arbitrary filenames or
// missing external evidence. The owner must resolve CURRENT_COMPLETION for every key and enforce
// lease, pending-work, deadline, replay and graph-budget checks before creating a fresh revision;
// this helper neither accepts a business attempt nor consumes a budget or creates a continuation.
// DecodeCaptureProgress проверяет JSON-запрос одного технического продолжения в SERVER_DAG_V1.
// Ключи выбирают непустое подмножество опубликованных ARTIFACT requirements, а не произвольные
// имена файлов или недостающие внешние доказательства. Владелец разрешает CURRENT_COMPLETION
// каждого ключа и проверяет lease, незавершённую работу, deadline, replay и бюджет графа до fresh
// revision. Функция не принимает бизнес-попытку, не расходует бюджет и не создаёт продолжение.
func DecodeCaptureProgress(raw, executionMode string, requirements []entity.WorkflowResultRequirement) (entity.WorkflowCaptureProgressDeclaration, error) {
	invalid := entity.WorkflowCaptureProgressDeclaration{}
	if executionMode != ServerDAGV1 || len(raw) > MaximumResultBytes || !utf8.ValidString(raw) || !uniqueJSON([]byte(raw)) {
		return invalid, ErrResultInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &fields) != nil || !exactFields(fields, "schema", "phase", "cause", "artifactKeys") {
		return invalid, ErrResultInvalid
	}
	var declaration entity.WorkflowCaptureProgressDeclaration
	if json.Unmarshal([]byte(raw), &declaration) != nil || declaration.Schema != CaptureProgressSchema ||
		declaration.Phase != CaptureProgressPhase || declaration.Cause != CaptureProgressCause ||
		len(declaration.ArtifactKeys) < 1 || len(declaration.ArtifactKeys) > 4 {
		return invalid, ErrResultInvalid
	}
	known, ok := captureProgressArtifacts(requirements)
	if !ok {
		return invalid, ErrResultInvalid
	}
	seen := map[string]bool{}
	for _, key := range declaration.ArtifactKeys {
		if _, allowed := known[key]; !allowed || seen[key] {
			return invalid, ErrResultInvalid
		}
		seen[key] = true
	}
	return declaration, nil
}

// captureProgressArtifacts builds an artifact-only selector registry without relaxing publication
// validation. Other result kinds cannot become capture keys; malformed or duplicate keys fail closed.
// captureProgressArtifacts строит реестр файловых селекторов без ослабления проверки публикации.
// Другие виды результата не становятся capture keys; повреждённые и повторные ключи отклоняются.
func captureProgressArtifacts(requirements []entity.WorkflowResultRequirement) (map[string]entity.WorkflowResultRequirement, bool) {
	if len(requirements) < 1 || len(requirements) > 4 {
		return nil, false
	}
	artifacts := map[string]entity.WorkflowResultRequirement{}
	seen := map[string]bool{}
	for _, requirement := range requirements {
		if !resultKey.MatchString(requirement.Key) || seen[requirement.Key] {
			return nil, false
		}
		seen[requirement.Key] = true
		if requirement.Kind == "ARTIFACT" {
			if !ValidRequirements([]entity.WorkflowResultRequirement{requirement}, nil) {
				return nil, false
			}
			artifacts[requirement.Key] = requirement
		}
	}
	return artifacts, len(artifacts) > 0
}
