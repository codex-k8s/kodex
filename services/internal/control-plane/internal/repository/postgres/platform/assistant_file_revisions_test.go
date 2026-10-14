package platform

import (
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func revisionOperationFixture() entity.AssistantPlanOperation {
	version := int64(3)
	item := entity.Artifact{Ref: "art_revision_fixture", CurrentRevisionRef: "arv_previous_fixture", FileName: "note.md", MediaType: "text/markdown",
		Digest: "sha256:" + strings.Repeat("1", 64), SizeBytes: 12, Revision: 2, Version: version, ScanState: "CLEAN", LifecycleState: "ACTIVE"}
	op := entity.AssistantPlanOperation{Key: "revision-note", Type: createProjectFileRevision, Action: "UPDATE", Title: "Обновить заметку", Summary: "Обновить текст заметки",
		Target: entity.AssistantPlanTarget{Kind: "ARTIFACT", Ref: item.Ref, Name: item.FileName, Version: &version}, Before: assistantRevisionBefore(item), ExpectedVersion: &version, Selected: true,
		Parameters: map[string]any{"artifactRef": item.Ref, "mediaType": "text/markdown", "contentEncoding": "UTF8", "contentRef": "pfcnt_fixture_revision", "digest": "sha256:" + strings.Repeat("2", 64), "sizeBytes": int64(7)}}
	op.After = assistantRevisionAfter(op)
	return op
}

func TestAssistantFileRevisionClosedTypedPlan(t *testing.T) {
	op := revisionOperationFixture()
	normalized, err := normalizeAssistantOperation(op)
	if err != nil {
		t.Fatal(err)
	}
	planned, err := assistantOperationCommand(normalized)
	if err != nil || planned.Kind != command.CreateProjectFileRevision {
		t.Fatalf("typed mapping: %v", err)
	}
	p := planned.Payload.(command.ProjectFileRevisionInput)
	if p.Content != nil || p.ArtifactRef != op.Target.Ref || p.SourceRevisionRef != "arv_previous_fixture" || p.ContentRef != "pfcnt_fixture_revision" || p.Prepared != nil {
		t.Fatal("durable plan contains body or lost immutable source pin")
	}
	for _, tamper := range []func(*entity.AssistantPlanOperation){
		func(op *entity.AssistantPlanOperation) { op.Target.Ref = "art_foreign_fixture" },
		func(op *entity.AssistantPlanOperation) { op.After["fileName"] = "renamed.md" },
		func(op *entity.AssistantPlanOperation) { op.Before["version"] = int64(4) },
		func(op *entity.AssistantPlanOperation) { op.Action = "CREATE" },
	} {
		bad := revisionOperationFixture()
		tamper(&bad)
		if _, err := normalizeAssistantOperation(bad); err == nil {
			t.Fatal("tampered immutable projection accepted")
		}
	}
}

func TestAssistantFilePersistenceRejectsUnstagedBody(t *testing.T) {
	op := revisionOperationFixture()
	prepared := map[string]string{op.Key: "ledger_fixture"}
	if err := assistantFilePersistenceReady([]entity.AssistantPlanOperation{op}, prepared); err != nil {
		t.Fatal(err)
	}
	for _, tamper := range []func(*entity.AssistantPlanOperation){
		func(op *entity.AssistantPlanOperation) { op.Parameters["content"] = "body" },
		func(op *entity.AssistantPlanOperation) { op.After["content"] = "body" },
		func(op *entity.AssistantPlanOperation) { op.Input = map[string]any{"content": "body"} },
	} {
		bad := revisionOperationFixture()
		tamper(&bad)
		if assistantFilePersistenceReady([]entity.AssistantPlanOperation{bad}, prepared) == nil {
			t.Fatal("file body crossed SQL persistence boundary")
		}
	}
	if assistantFilePersistenceReady([]entity.AssistantPlanOperation{op}, nil) == nil {
		t.Fatal("caller-created contentRef crossed SQL persistence boundary")
	}
}

func TestAssistantFileRevisionRawContentBoundary(t *testing.T) {
	for _, body := range []string{"новый текст", "", strings.Repeat("a", 1<<20)} {
		op := revisionOperationFixture()
		op.Parameters = map[string]any{"artifactRef": op.Target.Ref, "mediaType": "text/markdown", "content": "" + body}
		op.After = assistantRevisionAfter(op)
		normalized, err := normalizeAssistantOperation(op)
		if err != nil {
			t.Fatal(err)
		}
		planned, err := assistantOperationCommand(normalized)
		if err != nil {
			t.Fatal(err)
		}
		p := planned.Payload.(command.ProjectFileRevisionInput)
		if string(p.Content) != body || p.SizeBytes != int64(len(body)) || !objectDigestValid(p.Digest) || assistantProjectFileContentReady(normalized) {
			t.Fatal("raw proposal treated as durable prepared content")
		}
	}
	for _, body := range []string{"a\x00b", string([]byte{0xff}), strings.Repeat("a", 1<<20+1)} {
		op := revisionOperationFixture()
		op.Parameters = map[string]any{"artifactRef": op.Target.Ref, "mediaType": "text/markdown", "content": body}
		op.After = assistantRevisionAfter(op)
		normalized, err := normalizeAssistantOperation(op)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := assistantOperationCommand(normalized); err == nil {
			t.Fatal("invalid bounded UTF8 body accepted")
		}
	}
}

func TestAssistantFileRevisionDraftPreservesImmutableBaseline(t *testing.T) {
	original := revisionOperationFixture()
	edited := revisionOperationFixture()
	edited.Parameters = map[string]any{"artifactRef": original.Target.Ref, "mediaType": "text/plain", "content": "updated"}
	updated, err := rehydrateEditedAssistantFile(original, edited, false)
	if err != nil {
		t.Fatal(err)
	}
	if !assistantJSONEqual(updated.Before, original.Before) || !assistantJSONEqual(updated.Target, original.Target) || updated.ExpectedVersion != edited.ExpectedVersion {
		t.Fatal("owner edit rebased immutable source")
	}
	bad := revisionOperationFixture()
	bad.Parameters["mediaType"] = "text/plain"
	if _, err := rehydrateEditedAssistantFile(original, bad, false); err == nil {
		t.Fatal("metadata edit without new content accepted")
	}
	bad = revisionOperationFixture()
	bad.Before["currentRevisionRef"] = "arv_latest_unapproved"
	if _, err := rehydrateEditedAssistantFile(original, bad, false); err == nil {
		t.Fatal("latest fallback accepted")
	}
	bad = revisionOperationFixture()
	bad.After["digest"] = "sha256:" + strings.Repeat("f", 64)
	if _, err := rehydrateEditedAssistantFile(original, bad, false); err == nil {
		t.Fatal("owner supplied server digest accepted")
	}
}

func TestAssistantFilePlanHistoryRedactsRawBodyWithoutMutatingHistory(t *testing.T) {
	parameters := map[string]any{"content": "historical body", "fileName": "note.md"}
	operations := []entity.AssistantPlanOperation{{Type: "CREATE_PROJECT_FILE", Parameters: parameters, After: parameters}}
	redactAssistantFilePlanOperations(operations)
	if _, ok := operations[0].Parameters["content"]; ok {
		t.Fatal("raw body leaked in public history")
	}
	if _, ok := operations[0].After["content"]; ok {
		t.Fatal("raw body leaked in after projection")
	}
	if parameters["content"] != "historical body" {
		t.Fatal("immutable stored history input mutated")
	}
}
