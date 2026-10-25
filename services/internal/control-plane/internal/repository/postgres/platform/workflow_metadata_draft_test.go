package platform

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// workflowMetadataDraftFixture mirrors the stored default produced by a metadata-only UI create,
// without inserting a synthetic executable step or published revision into the draft.
// workflowMetadataDraftFixture воспроизводит сохранённый default metadata-only UI create,
// не добавляя синтетический исполнимый этап либо опубликованную ревизию.
func workflowMetadataDraftFixture() entity.WorkflowVersion {
	return entity.WorkflowVersion{Ref: "draft", Name: "Ответ поддержки в пределах срока согласования — QA1830", Purpose: "Подготовить короткий ответ и передать владельцу на согласование.", CoordinatorAgentRef: "agt_metadata_fixture", VersionNumber: 1, Concurrency: 1, TimeoutSeconds: 3600, ResultSchema: map[string]any{}}
}

// workflowMetadataDraftRow serializes one exact synthetic draft into the actor-scoped Get/List row.
// workflowMetadataDraftRow сериализует точный синтетический draft в actor-scoped строку Get/List.
func workflowMetadataDraftRow(t *testing.T, draft entity.WorkflowVersion) workflowRepairRow {
	t.Helper()
	raw, err := json.Marshal(draft)
	if err != nil {
		t.Fatal("encode metadata-only workflow")
	}
	return workflowRepairRow{"wfl_metadata_fixture", "prj_metadata_fixture", draft.Name, draft.Purpose, draft.CoordinatorAgentRef, "DRAFT", int64(1), raw, []byte(nil), int32(0), time.Time{}, time.Time{}, "", true, true}
}

// TestWorkflowMetadataDraftReadBoundary reproduces empty default-draft readback and separates it
// from strict publication/execution validation; corrupt data and empty published snapshots stay closed.
// TestWorkflowMetadataDraftReadBoundary воспроизводит чтение пустого default draft, отделяя его
// от строгой проверки публикации/исполнения; повреждённые данные и пустой published snapshot закрыты.
func TestWorkflowMetadataDraftReadBoundary(t *testing.T) {
	draft := workflowMetadataDraftFixture()
	t.Run("metadata-only-default-is-readable", func(t *testing.T) {
		item, err := scanWorkflow(workflowMetadataDraftRow(t, draft), true)
		if err != nil || item.Draft == nil || item.Published != nil || len(item.Draft.Steps) != 0 || item.Draft.TimeoutSeconds != 3600 || !contains(item.NextActions, "EDIT") {
			t.Fatal("default metadata-only draft cannot reach its editor")
		}
		if validWorkflowVersion(*item.Draft) || workflowVersionStructureInvalidField(*item.Draft) == "" {
			t.Fatal("empty draft became executable")
		}
		if _, err := assistantWorkflowMetadataJSON(item); err != nil {
			t.Fatal("metadata-only workflow cannot be inspected by its assistant")
		}
	})
	t.Run("published-empty-is-rejected", func(t *testing.T) {
		row := workflowMetadataDraftRow(t, draft)
		row[5], row[8], row[9], row[12] = "PUBLISHED", row[7], int32(1), "wfv_metadata_fixture"
		valid := validWorkflowFixture()
		row[7], _ = json.Marshal(valid)
		if _, err := scanWorkflow(row, true); !errors.Is(err, errs.ErrUnavailable) {
			t.Fatal("empty published workflow accepted")
		}
	})
	t.Run("archive-empty-draft-remains-readable", func(t *testing.T) {
		row := workflowMetadataDraftRow(t, draft)
		row[5] = "ARCHIVED"
		item, err := scanWorkflow(row, true)
		if err != nil || item.Draft == nil || contains(item.NextActions, "EDIT") || contains(item.NextActions, "LAUNCH") {
			t.Fatal("archived empty draft lost its closed readback")
		}
	})
	for _, state := range []string{"VALID", "PUBLISHED"} {
		t.Run(state+"-empty-draft-rejected", func(t *testing.T) {
			row := workflowMetadataDraftRow(t, draft)
			row[5] = state
			if _, err := scanWorkflow(row, true); !errors.Is(err, errs.ErrUnavailable) {
				t.Fatal("empty validated/published draft accepted")
			}
		})
	}
	for _, scenario := range []string{"corrupt-json", "oversized-name", "invalid-timeout", "incomplete-step"} {
		t.Run(scenario, func(t *testing.T) {
			candidate := draft
			switch scenario {
			case "oversized-name":
				candidate.Name = strings.Repeat("Я", 161)
			case "invalid-timeout":
				candidate.TimeoutSeconds = 0
			case "incomplete-step":
				candidate.Steps = []entity.WorkflowStep{{Key: "unfinished"}}
			}
			row := workflowMetadataDraftRow(t, candidate)
			if scenario == "corrupt-json" {
				row[7] = []byte("{")
			}
			if _, err := scanWorkflow(row, true); !errors.Is(err, errs.ErrUnavailable) {
				t.Fatal("corrupt or incomplete stored draft accepted")
			}
		})
	}
}
