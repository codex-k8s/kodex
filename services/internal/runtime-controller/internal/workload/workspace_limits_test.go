package workload

import (
	"reflect"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"k8s.io/client-go/kubernetes/fake"
)

func TestWorkspaceLimitsConsumerPreservesExactQuotaAndResourceDigest(t *testing.T) {
	base := runtimecontract.DefaultRuntimeEnvironmentPolicy()
	base.Resources.WorkspaceLimits = &runtimecontract.RuntimeWorkspaceLimits{MaxBytes: 4096, MaxFiles: 10}
	policy, err := runtimecontract.NormalizeRuntimeEnvironmentPolicy(base)
	if err != nil {
		t.Fatal(err)
	}
	wire := testRuntimeEnvironmentPolicyProto(policy)
	wire.Resources.WorkspaceLimits = &controlplanev1.RuntimeWorkspaceLimits{MaxBytes: 4096, MaxFiles: 10}
	actual, err := runtimeEnvironmentPolicyFromProto(wire)
	if err != nil || !reflect.DeepEqual(actual.Resources, policy.Resources) || actual.ResourcesDigest != policy.ResourcesDigest {
		t.Fatal("consumer lost immutable workspace budget")
	}
	wire.Resources.WorkspaceLimits.MaxFiles = 11
	if _, err := runtimeEnvironmentPolicyFromProto(wire); err == nil {
		t.Fatal("quota mismatch bypassed resource digest")
	}
	wire.Resources.WorkspaceLimits.MaxFiles = runtimecontract.RuntimeWorkspaceMaximumFiles + 1
	if _, err := runtimeEnvironmentPolicyFromProto(wire); err == nil {
		t.Fatal("consumer accepted excessive quota")
	}
}

func TestWorkspaceLimitsWarmAndTurnImmutableMaterialization(t *testing.T) {
	manager := newTestManager(t, fake.NewSimpleClientset())
	turn := testExecution(true)
	warm := testWarmRevision()
	limits := &runtimecontract.RuntimeWorkspaceLimits{MaxBytes: 4096, MaxFiles: 10}
	for _, revision := range []*controlplanev1.RuntimeRevisionSnapshot{turn.Revision, warm} {
		policy, err := runtimeEnvironmentPolicyFromProto(revision.EnvironmentPolicy)
		if err != nil {
			t.Fatal(err)
		}
		policy.Resources.WorkspaceLimits = limits
		policy, _ = runtimecontract.NormalizeRuntimeEnvironmentPolicy(policy)
		revision.EnvironmentPolicy = testRuntimeEnvironmentPolicyProto(policy)
		revision.EnvironmentPolicy.Resources.WorkspaceLimits = &controlplanev1.RuntimeWorkspaceLimits{MaxBytes: limits.MaxBytes, MaxFiles: limits.MaxFiles}
		workspace, _ := runtimecontract.RuntimeWorkspacePolicyWithLimits(limits)
		revision.WorkspacePolicy.MaximumWritableBytes, revision.WorkspacePolicy.MaximumFileCount = workspace.MaximumWritableBytes, workspace.MaximumFileCount
		revision.WorkspacePolicy.Digest = workspace.Digest
		image, tools := runtimeEnvironmentContract(revision)
		revision.RuntimeEnvironmentDigest, _ = runtimecontract.RuntimeEnvironmentDigest(nil, nil, image, tools, policy)
	}
	sealTestTurnExecution(turn)
	sealTestWarmRevision(warm)
	turnInput, _, err := manager.BuildTurnInput(turn)
	if err != nil {
		t.Fatal(err)
	}
	warmInput, _, err := manager.BuildWarmInput(warm)
	if err != nil || !reflect.DeepEqual(turnInput.WorkspacePolicy, warmInput.WorkspacePolicy) ||
		turnInput.WorkspacePolicy.MaximumWritableBytes != 4096 || turnInput.WorkspacePolicy.MaximumFileCount != 10 {
		t.Fatal("warm and turn did not consume same immutable quota")
	}
	defaults := runtimecontract.RuntimeWorkspacePolicyV1()
	turn.Revision.WorkspacePolicy.MaximumWritableBytes = defaults.MaximumWritableBytes
	turn.Revision.WorkspacePolicy.MaximumFileCount = defaults.MaximumFileCount
	turn.Revision.WorkspacePolicy.Digest = defaults.Digest
	sealTestTurnExecution(turn)
	if _, _, err := manager.BuildTurnInput(turn); err == nil {
		t.Fatal("fresh signed revision ignored bounded workspace quota")
	}
}
