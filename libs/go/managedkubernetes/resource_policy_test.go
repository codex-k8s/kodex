package managedkubernetes

import (
	"encoding/json"
	"strings"
	"testing"
)

// resourcePolicyFixture assigns independent canonical pins to one CSI application volume and
// one explicitly classified controller; these are metadata fixtures, not live qualifications.
// resourcePolicyFixture назначает независимые canonical pins одному CSI application volume
// и одному explicitly classified controller; это metadata fixtures, не live qualifications.
func resourcePolicyFixture() ResourcePolicy {
	return ResourcePolicy{Storage: []StorageQualification{{Provider: StorageProviderCSIAppDataV1,
		PVCNamespace: "application", PVCName: "data", PVCUID: "11111111-1111-4111-8111-111111111111",
		PVName: "application-volume", PVUID: "22222222-2222-4222-8222-222222222222", StorageClassName: "qualified-data",
		StorageClassUID: "33333333-3333-4333-8333-333333333333", Provisioner: "csi.example.test", CSIDriver: "csi.example.test",
		CSIDriverUID: "44444444-4444-4444-8444-444444444444", CSIDriverSpecSHA256: strings.Repeat("a", 64),
		VolumeSourceSHA256: strings.Repeat("b", 64), MountOptionsSHA256: strings.Repeat("c", 64), VolumeMode: "Filesystem", MountReadOnly: false}},
		Controllers: []ControllerQualification{{Group: "custom.example.test", Version: "v1", Resource: "workers", Kind: "Worker",
			SchemaSHA256: strings.Repeat("d", 64), ControllerKind: "Deployment", ControllerNamespace: "controllers", ControllerName: "worker-controller",
			ControllerUID: "55555555-5555-4555-8555-555555555555", ControllerContainer: "manager", ControllerImageDigest: strings.Repeat("e", 64),
			TemplateSHA256: strings.Repeat("f", 64), Consumer: ControllerConsumerPodTemplateV1, Extractor: "SPEC_TEMPLATE"}}}
}

// TestResourcePolicyExactCanonicalMetadata rejects caller-shaped switches and mixed pins before
// they can enter a private target snapshot. A successful decode is only metadata syntax proof.
// TestResourcePolicyExactCanonicalMetadata отклоняет caller-shaped switches и mixed pins до
// private target snapshot. Успешный decode доказывает только metadata syntax.
func TestResourcePolicyExactCanonicalMetadata(t *testing.T) {
	policy := resourcePolicyFixture()
	raw, err := json.Marshal(policy)
	if err != nil || policy.Validate() != nil {
		t.Fatal("owned typed policy was rejected")
	}
	decoded, digest, err := DecodeResourcePolicy(raw)
	if err != nil || !ValidDigest(digest) || decoded.Storage[0].PVCUID != policy.Storage[0].PVCUID {
		t.Fatal("canonical typed policy lost its pins")
	}
	for _, invalid := range [][]byte{
		append(append([]byte{}, raw...), '\n'),
		[]byte(`{"storage":[],"controllers":[],"allow":true}`),
		[]byte(`{"storage":[],"storage":[]}`),
		[]byte(`{"Storage":[]}`),
		[]byte("{\"storage\":\"\xff\"}"),
	} {
		if _, _, err := DecodeResourcePolicy(invalid); err == nil {
			t.Fatal("ambiguous or noncanonical profile was admitted")
		}
	}
}

// TestResourcePolicyRejectsUnqualifiedProvidersAndControllers ensures unsupported storage,
// arbitrary extractors, missing template/source evidence and duplicate routes stay closed.
// TestResourcePolicyRejectsUnqualifiedProvidersAndControllers проверяет closed отказ при
// unsupported storage, arbitrary extractors, missing template/source evidence и duplicate routes.
func TestResourcePolicyRejectsUnqualifiedProvidersAndControllers(t *testing.T) {
	for _, mutation := range []struct {
		name string
		edit func(*ResourcePolicy)
	}{
		{"hostPath provider", func(policy *ResourcePolicy) { policy.Storage[0].Provider = "HOST_PATH" }},
		{"foreign provisioner", func(policy *ResourcePolicy) { policy.Storage[0].Provisioner = "foreign.example.test" }},
		{"missing volume source pin", func(policy *ResourcePolicy) { policy.Storage[0].VolumeSourceSHA256 = "" }},
		{"missing driver spec pin", func(policy *ResourcePolicy) { policy.Storage[0].CSIDriverSpecSHA256 = "" }},
		{"zero PVC witness", func(policy *ResourcePolicy) { policy.Storage[0].PVCUID = "00000000-0000-0000-0000-000000000000" }},
		{"unknown volume mode", func(policy *ResourcePolicy) { policy.Storage[0].VolumeMode = "AUTO" }},
		{"duplicate claim", func(policy *ResourcePolicy) { policy.Storage = append(policy.Storage, policy.Storage[0]) }},
		{"missing controller template", func(policy *ResourcePolicy) { policy.Controllers[0].TemplateSHA256 = "" }},
		{"arbitrary JSONPath", func(policy *ResourcePolicy) { policy.Controllers[0].Extractor = "$.spec.anything" }},
		{"unknown controller consumer", func(policy *ResourcePolicy) { policy.Controllers[0].Consumer = "ALLOW" }},
		{"data-only template mismatch", func(policy *ResourcePolicy) { policy.Controllers[0].Consumer = ControllerConsumerDataOnlyV1 }},
		{"foreign executable kind", func(policy *ResourcePolicy) { policy.Controllers[0].ControllerKind = "Job" }},
		{"duplicate GVR", func(policy *ResourcePolicy) { policy.Controllers = append(policy.Controllers, policy.Controllers[0]) }},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			policy := resourcePolicyFixture()
			mutation.edit(&policy)
			if policy.Validate() == nil {
				t.Fatal("unqualified profile metadata was admitted")
			}
		})
	}
	policy := resourcePolicyFixture()
	policy.Controllers[0].Consumer, policy.Controllers[0].Extractor = ControllerConsumerDataOnlyV1, "NONE"
	if policy.Validate() != nil {
		t.Fatal("explicit operator data-only classification was rejected")
	}
}
