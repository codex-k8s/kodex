package admissioncontroller

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestReportMaterializationBuildCopyAndToolRegistry(t *testing.T) {
	root := repositoryRoot()
	for _, name := range []string{"tools/dev/Dockerfile.local-image-supply-chain", "infra/admission-tools/Dockerfile", "services/jobs/role-image-builder/Dockerfile"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		for _, fragment := range []string{
			"-o /out/image-vulnerability-report-validator ./cmd/image-vulnerability-report-validator",
			"COPY --from=build --chmod=0555 /out/image-vulnerability-report-validator /usr/local/bin/image-vulnerability-report-validator",
		} {
			if strings.Count(string(data), fragment) != 1 {
				t.Fatalf("%s must build and materialize the exact validator once", name)
			}
		}
		if !strings.Contains(string(data), "image-vulnerability-report-validator jq") && !strings.Contains(string(data), "RUN command -v image-vulnerability-report-validator >/dev/null") {
			t.Fatalf("%s is missing the executable probe", name)
		}
	}
	required := completeTestPolicy().Data["requiredTools"]
	tools := strings.Split(required, ",")
	if !slices.IsSorted(tools) || len(slices.Compact(slices.Clone(tools))) != len(tools) || !slices.Contains(tools, "image-vulnerability-report-validator") || !slices.Contains(tools, "tr") {
		t.Fatal("required tools must be sorted, unique and complete")
	}
	data, err := os.ReadFile(filepath.Join(root, "deploy/k8s/base/image-supply-chain/admission-policy-parameters.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	var schema map[string]any
	if err := decoder.Decode(&schema); err != nil {
		t.Fatal(err)
	}
	spec := schema["spec"].(map[string]any)
	version := spec["versions"].([]any)[0].(map[string]any)
	openAPI := version["schema"].(map[string]any)["openAPIV3Schema"].(map[string]any)
	properties := openAPI["properties"].(map[string]any)["spec"].(map[string]any)["properties"].(map[string]any)
	enum := properties["requiredTools"].(map[string]any)["enum"]
	if !reflect.DeepEqual(enum, []any{required}) {
		t.Fatal("CRD must admit only the exact required tool registry")
	}
}

func TestReportMaterializationRenderedClaimEvidenceNetworkIsExact(t *testing.T) {
	command := exec.CommandContext(t.Context(), "kubectl", "kustomize", filepath.Join(repositoryRoot(), "deploy/k8s/overlays/staging/image-supply-chain"))
	command.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin"}
	data, err := command.Output()
	if err != nil {
		t.Fatalf("local environment render failed: %v", err)
	}
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	policies := map[string]networkingv1.NetworkPolicy{}
	var ownerPolicy corev1.ConfigMap
	var parameters struct {
		Spec struct {
			RequiredTools string `json:"requiredTools"`
		} `json:"spec"`
	}
	for {
		var object json.RawMessage
		if err := decoder.Decode(&object); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		var header struct {
			Kind     string            `json:"kind"`
			Metadata metav1.ObjectMeta `json:"metadata"`
		}
		if err := json.Unmarshal(object, &header); err != nil {
			t.Fatal(err)
		}
		switch header.Kind {
		case "NetworkPolicy":
			var policy networkingv1.NetworkPolicy
			if err := json.Unmarshal(object, &policy); err != nil {
				t.Fatal(err)
			}
			policies[policy.Name] = policy
		case "ConfigMap":
			if header.Metadata.Name == "kodex-image-admission-policy" {
				if err := json.Unmarshal(object, &ownerPolicy); err != nil {
					t.Fatal(err)
				}
			}
		case "ImageAdmissionPolicyParameters":
			if err := json.Unmarshal(object, &parameters); err != nil {
				t.Fatal(err)
			}
		}
	}
	if ownerPolicy.Data["requiredTools"] != completeTestPolicy().Data["requiredTools"] || parameters.Spec.RequiredTools != ownerPolicy.Data["requiredTools"] {
		t.Fatal("rendered owner policy and parameter registry differ")
	}
	claim := policies["kodex-image-admission-claim-evidence-exact-path"]
	expectedLabels := map[string]string{"app.kubernetes.io/name": "kodex-image-admission", "app.kubernetes.io/component": "image-admission", "kodex.dev/image-admission-phase": "claim"}
	if claim.Namespace != "kodex-system" || !reflect.DeepEqual(claim.Spec.PodSelector.MatchLabels, expectedLabels) || len(claim.Spec.PodSelector.MatchExpressions) != 0 || len(claim.Spec.Ingress) != 0 || !reflect.DeepEqual(claim.Spec.PolicyTypes, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress}) || len(claim.Spec.Egress) != 1 {
		t.Fatal("claim evidence policy scope is not exact")
	}
	rule := claim.Spec.Egress[0]
	if len(rule.To) != 1 || len(rule.Ports) != 1 || rule.Ports[0].Protocol == nil || *rule.Ports[0].Protocol != corev1.ProtocolTCP || rule.Ports[0].Port == nil || rule.Ports[0].Port.IntVal != 5007 || rule.Ports[0].EndPort != nil {
		t.Fatal("claim evidence endpoint is not exact TCP 5007")
	}
	peer := rule.To[0]
	if peer.NamespaceSelector != nil || peer.IPBlock != nil || peer.PodSelector == nil || len(peer.PodSelector.MatchExpressions) != 0 || !reflect.DeepEqual(peer.PodSelector.MatchLabels, map[string]string{"app.kubernetes.io/name": "kodex-image-registry", "kodex.dev/registry-scope": "evidence"}) {
		t.Fatal("claim evidence destination is not the exact same-namespace registry")
	}
	registry := policies["kodex-image-registry-evidence"]
	if len(registry.Spec.Ingress) != 1 || len(registry.Spec.Ingress[0].From) != 1 || len(registry.Spec.Ingress[0].Ports) != 1 || registry.Spec.Ingress[0].Ports[0].Port == nil || registry.Spec.Ingress[0].Ports[0].Port.IntVal != 5007 || registry.Spec.Ingress[0].Ports[0].Protocol == nil || *registry.Spec.Ingress[0].Ports[0].Protocol != corev1.ProtocolTCP {
		t.Fatal("evidence registry ingress is not exact")
	}
	source := registry.Spec.Ingress[0].From[0]
	if source.PodSelector == nil || source.NamespaceSelector != nil || source.IPBlock != nil {
		t.Fatal("registry source is not exact same-namespace workload")
	}
	selector, err := metav1.LabelSelectorAsSelector(source.PodSelector)
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range phases {
		podLabels := labels.Set{"app.kubernetes.io/name": "kodex-image-admission", "app.kubernetes.io/component": "image-admission", "kodex.dev/image-admission-phase": phase}
		if selector.Matches(podLabels) != slices.Contains([]string{"claim", "admit", "promote"}, phase) {
			t.Fatal("registry ingress expanded to a scanner or signer")
		}
	}
}
