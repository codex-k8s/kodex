package platform

import (
	"testing"

	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// Каждая launch fixture получает pins тем же leased catalog, что native tool.
func workflowCatalogLaunchInput(t *testing.T, service *serviceplatform.Service, principal value.Principal, lease map[string]any, ref, task string) command.LaunchWorkflowInput {
	t.Helper()
	principal.Permission = "platform.runtime.execution.workflow.catalog"
	input := query.ExecutionWorkflowCatalog{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation")}
	for pages := 0; pages < 10; pages++ {
		result, err := service.GetExecutionWorkflowCatalog(t.Context(), principal, input)
		if err != nil {
			t.Fatal("execution workflow catalog", err)
		}
		if len(result.Items) > 10 {
			t.Fatal("catalog page exceeded limit")
		}
		for _, item := range result.Items {
			if item.WorkflowRef == ref {
				if item.PublishedRef == "" || !workflowSpecDigestPattern.MatchString(item.SpecDigest) || item.Readiness.RevisionRef != item.PublishedRef {
					t.Fatal("catalog pins missing")
				}
				return command.LaunchWorkflowInput{LeaseRef: input.LeaseRef, Fence: input.Fence, Generation: input.Generation, WorkflowRef: ref, Task: task, ExpectedPublishedRef: item.PublishedRef, ExpectedSpecDigest: item.SpecDigest, ExpectedWorkflowVersion: item.WorkflowVersion}
			}
		}
		if result.NextPageToken == "" {
			break
		}
		input.PageToken = result.NextPageToken
	}
	t.Fatal("published workflow absent from eligible catalog")
	return command.LaunchWorkflowInput{}
}
