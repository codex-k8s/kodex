package platform

import (
	"context"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testWorkflowCurrentArtifactSelection exercises the real owner query after completion, including
// exact pins and refusal to interpret a newly produced file as an earlier frozen-catalog artifact.
// It does not prove the future continuation dispatcher or earlier-producer positive path.
// testWorkflowCurrentArtifactSelection проверяет настоящий owner query после completion: exact pins
// и отказ считать новый файл прежним артефактом frozen catalog. Тест не доказывает будущий dispatcher
// продолжений или положительный путь раннего производителя.
func testWorkflowCurrentArtifactSelection(t *testing.T, ctx context.Context, pool *pgxpool.Pool, nodeRef, leaseRef string, capture map[string]any) {
	t.Helper()
	current, lease, _ := workflowJournalFixture(t, ctx, pool, nodeRef, leaseRef)
	entries := capture["artifacts"].([]any)
	for _, fault := range []string{"current", "wrong-name", "wrong-media", "wrong-root", "wrong-revision", "wrong-lease", "wrong-tenant", "captured-current", "current-with-pin"} {
		t.Run("artifact-selector-"+fault, func(t *testing.T) {
			scope := current
			coordinates := make(map[string]any, len(lease))
			for key, value := range lease {
				coordinates[key] = value
			}
			requirement := entity.WorkflowResultRequirement{Key: "report", Kind: "ARTIFACT", FileName: "capture.md", MediaType: "text/markdown"}
			declaration := entity.WorkflowDeclaredResult{Key: requirement.Key, Source: &entity.WorkflowArtifactSource{Kind: "CURRENT_COMPLETION"}}
			switch fault {
			case "wrong-name":
				requirement.FileName = "unrelated.md"
			case "wrong-media":
				requirement.MediaType = "application/json"
			case "wrong-root":
				coordinates["rootRunID"] = "00000000-0000-4000-8000-000000000001"
			case "wrong-revision":
				coordinates["runtimeRevisionID"] = "00000000-0000-4000-8000-000000000001"
			case "wrong-lease":
				coordinates["leaseID"] = "00000000-0000-4000-8000-000000000001"
			case "wrong-tenant":
				scope.organizationID = "00000000-0000-4000-8000-000000000001"
			case "captured-current":
				declaration.Source = &entity.WorkflowArtifactSource{Kind: "CAPTURED_ARTIFACT", ArtifactRef: "art_absent01", Revision: 1, Digest: "sha256:absent"}
				if len(entries) == 1 {
					entry := entries[0].(map[string]any)
					declaration.Source.ArtifactRef = entry["ref"].(string)
					declaration.Source.Digest = entry["digest"].(string)
				}
			case "current-with-pin":
				declaration.Source.ArtifactRef = "art_untrusted01"
			}
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal("begin artifact selector probe")
			}
			result, found, lookupErr := resolveWorkflowArtifactTx(ctx, tx, scope, coordinates, requirement, declaration)
			rollbackWorkflowJournalProbe(t, ctx, tx)
			if lookupErr != nil {
				t.Fatal("artifact selector query failed", completionOutcomeClass(lookupErr))
			}
			expected := fault == "current" && len(entries) == 1
			if found != expected {
				t.Fatal("artifact selector accepted the wrong completion evidence")
			}
			if found {
				entry := entries[0].(map[string]any)
				if result.Key != requirement.Key || result.Kind != requirement.Kind || result.ArtifactRef != entry["ref"] ||
					result.ArtifactRevision != entry["revisionRef"] || "sha256:"+result.Digest != entry["digest"] {
					t.Fatal("artifact selector changed immutable evidence pins")
				}
			}
		})
	}
}
