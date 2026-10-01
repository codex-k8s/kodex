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

func TestRuntimeEnvironmentPolicyNetworkDigestsMatchMigration(t *testing.T) {
	t.Parallel()

	withoutKubernetes := DefaultRuntimeEnvironmentPolicy()
	if withoutKubernetes.NetworkDigest != "7d4998b5d8c1db3a90002ea8e56bc4c1103a5facbf5eba9b313355f3b55ca765" {
		t.Fatalf("default network digest = %q", withoutKubernetes.NetworkDigest)
	}
	withKubernetes, err := RuntimeEnvironmentPolicyFromInput(RuntimeEnvironmentPolicyInput{
		Resources: withoutKubernetes.Resources,
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
	if withKubernetes.NetworkDigest != "de9f8cf5f3f75e49dc9d0df8e4776a33e17fbdc1501536d6284ec746342830d9" {
		t.Fatalf("Kubernetes network digest = %q", withKubernetes.NetworkDigest)
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

func TestRuntimeEnvironmentPolicyAllowsExactHTTPMethodSubset(t *testing.T) {
	t.Parallel()
	defaults := DefaultRuntimeEnvironmentPolicy()
	base := defaults
	base.Network.WebAccess = RuntimeWebAccess{
		Mode: RuntimeWebAccessAllowlistFull,
		Rules: []RuntimeWebAccessRule{{
			DomainPattern: "api.example.com",
			Protocol:      RuntimeWebProtocolHTTPS,
			Port:          443,
			HTTPMethods:   []string{RuntimeHTTPMethodGet, RuntimeHTTPMethodPost},
		}},
	}
	policy, err := NormalizeRuntimeEnvironmentPolicy(base)
	if err != nil {
		t.Fatal(err)
	}
	if !RuntimeWebAccessAllowsRequest(policy.Network.WebAccess, "api.example.com", RuntimeHTTPMethodPost) ||
		RuntimeWebAccessAllowsRequest(policy.Network.WebAccess, "api.example.com", RuntimeHTTPMethodDelete) {
		t.Fatalf("method policy = %#v", policy.Network.WebAccess)
	}

	invalid := []RuntimeWebAccessRule{
		{DomainPattern: "api.example.com", Protocol: RuntimeWebProtocolHTTPS, Port: 443},
		{DomainPattern: "api.example.com", Protocol: RuntimeWebProtocolHTTPS, Port: 443, HTTPMethods: []string{RuntimeHTTPMethodGet, RuntimeHTTPMethodGet}},
	}
	for _, rule := range invalid {
		candidate := defaults
		candidate.Network.WebAccess = RuntimeWebAccess{Mode: RuntimeWebAccessAllowlistFull, Rules: []RuntimeWebAccessRule{rule}}
		if _, err := NormalizeRuntimeEnvironmentPolicy(candidate); err == nil {
			t.Fatalf("invalid method policy was accepted: %#v", rule)
		}
	}
	readOnly := defaults
	readOnly.Network.WebAccess = RuntimeWebAccess{Mode: RuntimeWebAccessAllowlistReadOnly, Rules: []RuntimeWebAccessRule{{
		DomainPattern: "api.example.com", Protocol: RuntimeWebProtocolHTTPS, Port: 443,
		HTTPMethods: []string{RuntimeHTTPMethodPost},
	}}}
	if _, err := NormalizeRuntimeEnvironmentPolicy(readOnly); err == nil {
		t.Fatal("write method was accepted in read-only mode")
	}
}
