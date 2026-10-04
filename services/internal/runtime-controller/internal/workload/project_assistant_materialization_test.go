package workload

import (
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/protobuf/proto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestProjectAssistantMaterializesOnlyPinnedProjectTurn(t *testing.T) {
	api := fake.NewSimpleClientset()
	manager := newTestManager(t, api)
	execution := testExecution(true)
	execution.Revision.AssistantScope = controlplanev1.AssistantScope_ASSISTANT_SCOPE_PROJECT
	execution.Revision.AssistantProfileRef = "asstprof_abcdefgh"
	sealTestTurnExecution(execution)
	input, binding, err := manager.BuildTurnInput(execution)
	if err != nil {
		t.Fatal(err)
	}
	if input.AssistantScope != runtimecontract.AssistantScopeProject || input.AssistantProfileRef != execution.Revision.AssistantProfileRef || input.ProjectRef != execution.Run.ProjectRef || !input.IsAssistant() || input.IsSystemAssistant() {
		t.Fatal("project assistant identity was lost in owner materialization")
	}
	if err := manager.EnsureTurn(t.Context(), input, binding, testCredentialProjection(input)); err != nil {
		t.Fatal(err)
	}
	pods, err := api.CoreV1().Pods("kodex-runtime").List(t.Context(), metav1.ListOptions{})
	if err != nil || len(pods.Items) != 1 || pods.Items[0].Name != runtimecontract.RuntimeTurnPodName(input.LeaseRef) {
		t.Fatal("project assistant did not receive exactly its cold turn pod")
	}
	if _, _, err := manager.BuildWarmInput(execution.Revision); err == nil {
		t.Fatal("project assistant obtained organization warm runtime")
	}
	for name, mutate := range map[string]func(*controlplanev1.ClaimedExecution){
		"foreign profile": func(claim *controlplanev1.ClaimedExecution) { claim.Revision.AssistantProfileRef = "asstprof_other123" },
		"missing profile": func(claim *controlplanev1.ClaimedExecution) { claim.Revision.AssistantProfileRef = "" },
		"missing project": func(claim *controlplanev1.ClaimedExecution) { claim.Run.ProjectRef = "" },
		"scope escalation": func(claim *controlplanev1.ClaimedExecution) {
			claim.Revision.AssistantScope = controlplanev1.AssistantScope_ASSISTANT_SCOPE_SYSTEM
		},
		"unknown scope": func(claim *controlplanev1.ClaimedExecution) {
			claim.Revision.AssistantScope = controlplanev1.AssistantScope(99)
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := proto.Clone(execution).(*controlplanev1.ClaimedExecution)
			mutate(changed)
			if _, _, err := manager.BuildTurnInput(changed); err == nil {
				t.Fatal("detached project assistant authority reached materialization")
			}
		})
	}
}
