package workload

import (
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"k8s.io/client-go/kubernetes/fake"
)

func TestBuildTurnInputBindsRuntimeFileCatalogAndSchema(t *testing.T) {
	manager := newTestManager(t, fake.NewSimpleClientset())
	execution := testExecution(false)
	execution.Revision.FileCatalog = &cp.RuntimeFileCatalog{Ref: "vfc_filefixture1", Digest: strings.Repeat("e", 64), Total: 1,
		Purposes: []cp.RuntimeFilePurpose{cp.RuntimeFilePurpose_RUNTIME_FILE_PURPOSE_WORKSPACE_INPUT}}
	sealTestTurnExecution(execution)
	input, _, err := manager.BuildTurnInput(execution)
	if err != nil || input.FileCatalog == nil || input.FileCatalog.Ref != execution.Revision.FileCatalog.Ref {
		t.Fatalf("runtime file catalog hydration failed: %v", err)
	}
	compiled, _, _ := loadRunnerInputSchema(t)
	validateRunnerInputSchema(t, compiled, input)
	originalMCP := input.MCPBindingDigest
	input.FileCatalog.Digest = strings.Repeat("f", 64)
	_, changedMCP, err := runtimecontract.RuntimeExecutionBindingDigests(input)
	if err != nil || originalMCP == changedMCP {
		t.Fatal("file catalog substitution retained MCP binding")
	}
	execution.Revision.FileCatalog.Digest = strings.Repeat("f", 64)
	if _, _, err := manager.BuildTurnInput(execution); err == nil {
		t.Fatal("catalog changed without a new owner revision digest")
	}
	for _, purpose := range []cp.RuntimeFilePurpose{0, 99} {
		execution.Revision.FileCatalog.Purposes = []cp.RuntimeFilePurpose{purpose}
		if _, _, err := manager.BuildTurnInput(execution); err == nil {
			t.Fatal("unknown catalog purpose materialized")
		}
	}
	warm := testWarmRevision()
	warm.FileCatalog = &cp.RuntimeFileCatalog{Ref: "vfc_filefixture1", Digest: strings.Repeat("e", 64), Purposes: []cp.RuntimeFilePurpose{cp.RuntimeFilePurpose_RUNTIME_FILE_PURPOSE_WORKSPACE_INPUT}}
	if _, _, err := manager.BuildWarmInput(warm); err == nil {
		t.Fatal("warm runtime obtained an execution file grant")
	}
}
