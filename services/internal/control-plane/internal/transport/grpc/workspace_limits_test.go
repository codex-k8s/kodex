package grpc

import (
	"reflect"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func TestWorkspaceLimitsProtoPolicyRoundTripAndBounds(t *testing.T) {
	base := runtimecontract.DefaultRuntimeEnvironmentPolicy()
	base.Resources.WorkspaceLimits = &runtimecontract.RuntimeWorkspaceLimits{MaxBytes: 4096, MaxFiles: 10}
	policy, err := runtimecontract.NormalizeRuntimeEnvironmentPolicy(base)
	if err != nil {
		t.Fatal(err)
	}
	read := castRuntimeEnvironmentPolicy(policy)
	input := &controlplanev1.RuntimeEnvironmentPolicyInput{Resources: read.Resources, WebAccess: read.Network.WebAccess,
		KubernetesAccess: read.KubernetesAccess.Kind, NetworkDestinations: []controlplanev1.RuntimeNetworkDestination{
			controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_DNS,
			controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_PROVIDER_PROXY,
			controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_RUNTIME_CALLBACK,
		}}
	actual, err := domainRuntimeEnvironmentPolicy(input)
	if err != nil || !reflect.DeepEqual(actual.Resources, policy.Resources) || actual.ResourcesDigest != policy.ResourcesDigest {
		t.Fatal("typed policy transport lost signed quota")
	}
	read.Resources.WorkspaceLimits.MaxBytes = runtimecontract.RuntimeWorkspaceWritableBytes + 1
	if _, err := domainRuntimeEnvironmentPolicy(input); err == nil {
		t.Fatal("Proto caller exceeded platform maximum")
	}
}
