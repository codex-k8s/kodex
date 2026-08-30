package runtimecontract

import (
	"slices"
	"strings"
	"testing"
)

func TestRuntimeEnvironmentPolicyFromInputMaterializesExactBoundary(t *testing.T) {
	t.Parallel()
	defaults := DefaultRuntimeEnvironmentPolicy()
	policy, err := RuntimeEnvironmentPolicyFromInput(RuntimeEnvironmentPolicyInput{
		Resources: defaults.Resources,
		Volumes: []RuntimeVolume{{
			Name: "scratch", Kind: RuntimeVolumeEphemeralMemory, SizeMiB: 256,
		}},
		NetworkDestinations: []string{
			RuntimeEgressDNS,
			RuntimeEgressProviderProxy,
			RuntimeEgressRuntimeCallback,
			RuntimeEgressKubernetesAPI,
		},
		KubernetesAccess: RuntimeKubernetesAccessReadOwnExecution,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !policy.Network.DenyByDefault || len(policy.Network.Egress) != 5 ||
		policy.KubernetesAccess.Kind != RuntimeKubernetesAccessReadOwnExecution ||
		policy.Volumes[0].MountPath != "/workspace/.kodex/volumes/scratch" {
		t.Fatalf("policy = %#v", policy)
	}
	for _, digest := range []string{
		policy.ResourcesDigest,
		policy.VolumesDigest,
		policy.NetworkDigest,
		policy.RBACDigest,
	} {
		if len(digest) != 64 {
			t.Fatalf("invalid policy digest: %q", digest)
		}
	}

	access, err := RuntimeKubernetesAccessForExecution(
		policy.KubernetesAccess,
		"runtime-sa-a1b2c3d4",
		"runtime-turn-a1b2c3d4",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(access.Rules) != 2 || access.ServiceAccountName != "runtime-sa-a1b2c3d4" {
		t.Fatalf("access = %#v", access)
	}
	for _, rule := range access.Rules {
		if !slices.Equal(rule.Verbs, []string{"get"}) ||
			!slices.Equal(rule.ResourceNames, []string{"runtime-turn-a1b2c3d4"}) ||
			(rule.Resource != "pods" && rule.Resource != "pods/log") {
			t.Fatalf("rule exceeds execution boundary: %#v", rule)
		}
	}
	if err := ValidateRuntimeKubernetesAccess(access); err != nil {
		t.Fatal(err)
	}
	access.Rules[0].Verbs = []string{"list"}
	if err := ValidateRuntimeKubernetesAccess(access); err == nil {
		t.Fatal("tampered Kubernetes access was accepted")
	}
}

func TestRuntimeEnvironmentPolicyRejectsPrivilegeExpansion(t *testing.T) {
	t.Parallel()
	defaults := DefaultRuntimeEnvironmentPolicy()
	cases := map[string]RuntimeEnvironmentPolicy{
		"resource admission limit": func() RuntimeEnvironmentPolicy {
			value := defaults
			value.Resources.CPURequestMilli = 99
			return value
		}(),
		"host mount path": func() RuntimeEnvironmentPolicy {
			value := defaults
			value.Volumes = []RuntimeVolume{{Name: "scratch", Kind: RuntimeVolumeEphemeralDisk, SizeMiB: 64, MountPath: "/host"}}
			return value
		}(),
		"open network": func() RuntimeEnvironmentPolicy {
			value := defaults
			value.Network.DenyByDefault = false
			return value
		}(),
		"wildcard destination": func() RuntimeEnvironmentPolicy {
			value := defaults
			value.Network.Egress[0].Destination = "INTERNET"
			return value
		}(),
		"alternate namespace": func() RuntimeEnvironmentPolicy {
			value := defaults
			value.KubernetesAccess.Namespace = "default"
			return value
		}(),
	}
	for name, policy := range cases {
		name, policy := name, policy
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := NormalizeRuntimeEnvironmentPolicy(policy); err == nil {
				t.Fatal("expanded policy was accepted")
			}
		})
	}
}

func TestRuntimeEnvironmentPolicyInputRequiresClosedDestinationSet(t *testing.T) {
	t.Parallel()
	resources := DefaultRuntimeEnvironmentPolicy().Resources
	base := []string{RuntimeEgressDNS, RuntimeEgressProviderProxy, RuntimeEgressRuntimeCallback}
	if _, err := RuntimeEnvironmentPolicyFromInput(RuntimeEnvironmentPolicyInput{
		Resources: resources, NetworkDestinations: base,
		KubernetesAccess: RuntimeKubernetesAccessReadOwnExecution,
	}); err == nil || !strings.Contains(err.Error(), "destination") {
		t.Fatalf("missing Kubernetes API destination error = %v", err)
	}
	if _, err := RuntimeEnvironmentPolicyFromInput(RuntimeEnvironmentPolicyInput{
		Resources:           resources,
		NetworkDestinations: append(append([]string(nil), base...), RuntimeEgressKubernetesAPI),
		KubernetesAccess:    RuntimeKubernetesAccessNone,
	}); err == nil || !strings.Contains(err.Error(), "destination") {
		t.Fatalf("excess Kubernetes API destination error = %v", err)
	}
}
