package platform

import (
	"context"
	"encoding/hex"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func (repository *Repository) ListPromptContextVariables(ctx context.Context, principal value.Principal, filter query.Filter) (entity.PromptVariableCatalog, error) {
	selection := filter.TemplateContext
	if selection != nil && selection.TargetKind == "" && (selection.TargetRef != "" || selection.ExpectedContextDigest != "" || selection.Preview.AgentRef != "" || selection.Preview.WorkflowRevisionRef != "" || selection.Preview.WorkflowStageKey != "" || selection.Preview.ExpectedAgentVersion != 0 || selection.Preview.ExpectedWorkflowVersion != 0 || selection.Preview.AttachmentSetRef != "" || selection.Preview.Task != "" || len(selection.Preview.Input) != 0) {
		return entity.PromptVariableCatalog{}, errs.ErrInvalid
	}
	if selection == nil || selection.TargetKind == "" {
		items, total, next, err := repository.ListTemplateVariables(ctx, principal, filter)
		return entity.PromptVariableCatalog{Variables: items, Total: total, NextPageToken: next}, err
	}
	filter.Query = strings.TrimSpace(filter.Query)
	expectedAgent := selection.Preview.AgentRef
	if expectedAgent == "" && selection.TargetKind == "AGENT" {
		expectedAgent = selection.TargetRef
	}
	if !utf8.ValidString(filter.Query) || len([]rune(filter.Query)) > 200 || strings.ContainsRune(filter.Query, 0) || selection.RuntimeRevisionRef != "" ||
		selection.AgentRef != "" && selection.AgentRef != expectedAgent {
		return entity.PromptVariableCatalog{}, errs.ErrInvalid
	}
	if selection.ExpectedContextDigest != "" {
		digest, err := hex.DecodeString(selection.ExpectedContextDigest)
		if err != nil || len(digest) != 32 || strings.ToLower(selection.ExpectedContextDigest) != selection.ExpectedContextDigest {
			return entity.PromptVariableCatalog{}, errs.ErrInvalid
		}
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.PromptVariableCatalog{}, err
	}
	snapshot, err := repository.GetPromptPreviewContextSnapshot(ctx, principal, selection.TargetKind, selection.TargetRef, selection.Preview)
	if err != nil {
		return entity.PromptVariableCatalog{}, err
	}
	if filter.ProjectRef != "" && filter.ProjectRef != snapshot.ProjectRef {
		return entity.PromptVariableCatalog{}, errs.ErrNotFound
	}
	if selection.ExpectedContextDigest != "" && selection.ExpectedContextDigest != snapshot.ContextPin.Digest {
		return entity.PromptVariableCatalog{}, errs.ErrVersionMismatch
	}
	// Текущий digest входит в cursor независимо от caller pin. Изменение actor,
	// grants или selected revision инвалидирует следующую страницу.
	bound := *selection
	bound.ExpectedContextDigest = snapshot.ContextPin.Digest
	filter.TemplateContext = &bound
	cursor, err := decodeCatalogCursor(current, "TEMPLATE_VARIABLE", filter)
	if err != nil {
		return entity.PromptVariableCatalog{}, err
	}
	available := materializedVariableAvailability(snapshot)
	for name := range snapshot.UnavailableVariables {
		available[name] = false
	}
	for _, prefix := range []string{"input", "project", "workflow", "run", "session", "gate"} {
		scope, _ := snapshot.StructuredVariables[prefix].(map[string]any)
		if continuationNumber(scope["files_count"]) <= 0 {
			for _, suffix := range []string{"files", "files_count", "files_dir", "manifest_path"} {
				available[prefix+"."+suffix] = false
			}
		}
	}
	items := []entity.TemplateVariable{}
	needle := strings.ToLower(filter.Query)
	for _, item := range templateVariableCatalog() {
		item.Available = available[item.Name]
		item.Reason = variableAvailabilityReason(item, available, true)
		if reason := snapshot.UnavailableVariables[item.Name]; reason != "" {
			item.Reason = reason
			item.Available = false
		}
		if needle == "" || strings.Contains(strings.ToLower(item.Name+" "+item.Description), needle) {
			items = append(items, item)
		}
	}
	start := 0
	if cursor != "" {
		for index, item := range items {
			if item.Name == cursor {
				start = index + 1
				break
			}
		}
		if start == 0 {
			return entity.PromptVariableCatalog{}, errs.ErrInvalid
		}
	}
	end := min(start+int(boundedPage(filter.Page)), len(items))
	result := entity.PromptVariableCatalog{Variables: items[start:end], Total: int64(len(items)), ContextPin: snapshot.ContextPin}
	if end < len(items) {
		result.NextPageToken = encodeCatalogCursor(current, "TEMPLATE_VARIABLE", filter, items[end-1].Name)
	}
	return result, nil
}
