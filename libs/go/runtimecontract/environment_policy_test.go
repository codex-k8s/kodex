package runtimecontract

import (
	"reflect"
	"strings"
	"testing"
)

// TestEnvironmentWorkspaceLimitsDigestAndImmutableCopy pins narrower quotas into the resource digest,
// checks ownership of the copied limit object, and rejects quota expansion beyond the platform ceiling.
// TestEnvironmentWorkspaceLimitsDigestAndImmutableCopy закрепляет меньшие квоты в дайджесте ресурсов,
// проверяет отдельную копию лимитов и отклоняет превышение платформенного предела.
func TestEnvironmentWorkspaceLimitsDigestAndImmutableCopy(t *testing.T) {
	base := DefaultRuntimeEnvironmentPolicy()
	baseDigest := base.ResourcesDigest
	base.Resources.WorkspaceLimits = &RuntimeWorkspaceLimits{MaxBytes: 4096, MaxFiles: 10}
	limited, err := NormalizeRuntimeEnvironmentPolicy(base)
	if err != nil || limited.ResourcesDigest == baseDigest || limited.Resources.WorkspaceLimits == base.Resources.WorkspaceLimits {
		t.Fatal("workspace budget not bound or mutable input retained")
	}
	base.Resources.WorkspaceLimits.MaxBytes = 1
	if limited.Resources.WorkspaceLimits.MaxBytes != 4096 {
		t.Fatal("normalization retained caller-owned mutable quota")
	}
	limited.Resources.WorkspaceLimits = nil
	reset, err := NormalizeRuntimeEnvironmentPolicy(limited)
	if err != nil || reset.ResourcesDigest != baseDigest {
		t.Fatal("nil quota changed previous resources digest")
	}
	base.Resources.WorkspaceLimits.MaxFiles = RuntimeWorkspaceMaximumFiles + 1
	if _, err := NormalizeRuntimeEnvironmentPolicy(base); err == nil {
		t.Fatal("excessive workspace budget accepted")
	}
}

// TestRuntimeEnvironmentPolicyFromInputMaterializesExactBoundary verifies typed owner input becomes
// exact service egress, a fixed volume path, component digests, and an execution identity without API rules.
// TestRuntimeEnvironmentPolicyFromInputMaterializesExactBoundary проверяет превращение входа владельца
// в точные служебные связи, путь тома, дайджесты и идентичность исполнения без правил API.
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
		},
		KubernetesAccess: RuntimeKubernetesAccessNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !policy.Network.DenyByDefault || len(policy.Network.Egress) != 4 ||
		policy.KubernetesAccess.Kind != RuntimeKubernetesAccessNone ||
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
	if len(access.Rules) != 0 || access.ServiceAccountName != "runtime-sa-a1b2c3d4" {
		t.Fatalf("access = %#v", access)
	}
	if err := ValidateRuntimeKubernetesAccess(access); err != nil {
		t.Fatal(err)
	}
	access.Rules = []RuntimeKubernetesRule{{Resource: "pods", Verbs: []string{"get"}, ResourceNames: []string{"runtime-turn-a1b2c3d4"}}}
	if err := ValidateRuntimeKubernetesAccess(access); err == nil {
		t.Fatal("tampered Kubernetes access was accepted")
	}
}

// TestRuntimeEnvironmentPolicyNetworkDigestsMatchMigration protects the persisted default network digest.
// TestRuntimeEnvironmentPolicyNetworkDigestsMatchMigration защищает сохранённый дайджест сети по умолчанию.
func TestRuntimeEnvironmentPolicyNetworkDigestsMatchMigration(t *testing.T) {
	t.Parallel()

	withoutKubernetes := DefaultRuntimeEnvironmentPolicy()
	if withoutKubernetes.NetworkDigest != "7d4998b5d8c1db3a90002ea8e56bc4c1103a5facbf5eba9b313355f3b55ca765" {
		t.Fatalf("default network digest = %q", withoutKubernetes.NetworkDigest)
	}
}

// TestRuntimeEnvironmentDefaultEntryPointsKeepCanonicalDigests pins all persisted default digests
// across normalization and owner-input entrypoints and prevents shared mutable network defaults.
// TestRuntimeEnvironmentDefaultEntryPointsKeepCanonicalDigests закрепляет сохранённые default-дайджесты
// для нормализации и входа владельца и запрещает общие изменяемые сетевые настройки.
func TestRuntimeEnvironmentDefaultEntryPointsKeepCanonicalDigests(t *testing.T) {
	defaults := DefaultRuntimeEnvironmentPolicyWithoutDigests()
	for name, build := range map[string]func() (RuntimeEnvironmentPolicy, error){
		"direct": func() (RuntimeEnvironmentPolicy, error) { return DefaultRuntimeEnvironmentPolicy(), nil },
		"zero": func() (RuntimeEnvironmentPolicy, error) {
			return NormalizeRuntimeEnvironmentPolicy(RuntimeEnvironmentPolicy{})
		},
		"raw defaults": func() (RuntimeEnvironmentPolicy, error) { return NormalizeRuntimeEnvironmentPolicy(defaults) },
		"owner input": func() (RuntimeEnvironmentPolicy, error) {
			return RuntimeEnvironmentPolicyFromInput(RuntimeEnvironmentPolicyInput{
				Resources: defaults.Resources, KubernetesAccess: RuntimeKubernetesAccessNone,
				NetworkDestinations: []string{RuntimeEgressDNS, RuntimeEgressProviderProxy, RuntimeEgressRuntimeCallback},
			})
		},
	} {
		t.Run(name, func(t *testing.T) {
			policy, err := build()
			if err != nil || !reflect.DeepEqual(policy, DefaultRuntimeEnvironmentPolicy()) {
				t.Fatal("default policy entrypoints disagree")
			}
			actual := []string{policy.ResourcesDigest, policy.VolumesDigest, policy.NetworkDigest, policy.RBACDigest}
			expected := []string{
				"5cc6a50ed6896a06cea6acf2149f22c3d37fb52b6ef75f6305375d1bdde483b6",
				"d21f6a8d7834847c8c5e02c88ff36a1114ecbfdf43f0c6668fceff28334b3634",
				"7d4998b5d8c1db3a90002ea8e56bc4c1103a5facbf5eba9b313355f3b55ca765",
				"1b72c0704ea77cc1536f61d4e882306f12967fd39425fa6ea8d3f68500461d2e",
			}
			digest, digestErr := RuntimeEnvironmentPolicyDigest(policy)
			if !reflect.DeepEqual(actual, expected) || digestErr != nil || digest != "853fcfb2a8c5dccbbede567c4688264ca4f6762c117ee0cf271e8186038ff20e" {
				t.Fatal("persisted default policy digests changed")
			}
		})
	}
	other := DefaultRuntimeEnvironmentPolicyWithoutDigests()
	defaults.Network.Egress[0].Port = 1
	if other.Network.Egress[0].Port != 53 {
		t.Fatal("default policies share mutable egress")
	}
}

// TestRuntimeEnvironmentPolicyRejectsPrivilegeExpansion rejects resource, mount, network, destination,
// and namespace changes outside the platform's closed admission boundary.
// TestRuntimeEnvironmentPolicyRejectsPrivilegeExpansion отклоняет ресурсы, тома, сеть, назначения
// и пространство имён за пределами закрытой платформенной границы допуска.
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

// TestRuntimeEnvironmentPolicyInputRequiresClosedDestinationSet rejects unsupported Kubernetes access
// and any additional service destination before policy materialization.
// TestRuntimeEnvironmentPolicyInputRequiresClosedDestinationSet отклоняет неподдерживаемый Kubernetes-доступ
// и любое дополнительное служебное назначение до материализации политики.
func TestRuntimeEnvironmentPolicyInputRequiresClosedDestinationSet(t *testing.T) {
	t.Parallel()
	resources := DefaultRuntimeEnvironmentPolicy().Resources
	base := []string{RuntimeEgressDNS, RuntimeEgressProviderProxy, RuntimeEgressRuntimeCallback}
	if _, err := RuntimeEnvironmentPolicyFromInput(RuntimeEnvironmentPolicyInput{
		Resources: resources, NetworkDestinations: base,
		KubernetesAccess: "UNSUPPORTED",
	}); err == nil || !strings.Contains(err.Error(), "access") {
		t.Fatalf("retired Kubernetes access error = %v", err)
	}
	if _, err := RuntimeEnvironmentPolicyFromInput(RuntimeEnvironmentPolicyInput{
		Resources:           resources,
		NetworkDestinations: append(append([]string(nil), base...), "UNSUPPORTED"),
		KubernetesAccess:    RuntimeKubernetesAccessNone,
	}); err == nil || !strings.Contains(err.Error(), "destination") {
		t.Fatalf("excess Kubernetes API destination error = %v", err)
	}
}

// TestRetiredKubernetesAccessCannotIssueExecutionRules rejects unsupported profiles even with a valid
// self-consistent digest, preventing them from producing effective execution rules.
// TestRetiredKubernetesAccessCannotIssueExecutionRules отклоняет неподдерживаемые профили даже с верным
// согласованным дайджестом, запрещая им создавать эффективные правила исполнения.
func TestRetiredKubernetesAccessCannotIssueExecutionRules(t *testing.T) {
	t.Parallel()
	profile := RuntimeKubernetesAccessProfile{Kind: "UNSUPPORTED", Namespace: RuntimeKubernetesNamespace}
	if _, err := RuntimeKubernetesAccessForExecution(profile, "runtime-sa-a1b2c3d4", "runtime-turn-a1b2c3d4"); err == nil {
		t.Fatal("retired Kubernetes access issued execution rules")
	}
	access := RuntimeKubernetesAccess{Profile: profile, ServiceAccountName: "runtime-sa-a1b2c3d4", Rules: []RuntimeKubernetesRule{
		{Resource: "pods", Verbs: []string{"get"}, ResourceNames: []string{"runtime-turn-a1b2c3d4"}},
	}}
	access.Digest = digestRuntimeKubernetesAccess(access)
	if err := ValidateRuntimeKubernetesAccess(access); err == nil {
		t.Fatal("correctly digested unsupported rules were accepted for execution")
	}
	if _, err := RuntimeEnvironmentPolicyFromInput(RuntimeEnvironmentPolicyInput{
		Resources:           DefaultRuntimeEnvironmentPolicy().Resources,
		NetworkDestinations: []string{RuntimeEgressDNS, RuntimeEgressProviderProxy, RuntimeEgressRuntimeCallback},
		KubernetesAccess:    "UNSUPPORTED",
	}); err == nil {
		t.Fatal("unsupported Kubernetes access was accepted")
	}
}

// TestRuntimeEnvironmentPolicyAllowsExactHTTPMethodSubset preserves an explicit allowlist subset
// while rejecting empty/duplicate method lists and write methods in read-only mode.
// TestRuntimeEnvironmentPolicyAllowsExactHTTPMethodSubset сохраняет явное подмножество методов,
// отклоняя пустые и повторные списки и методы записи в режиме чтения.
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
