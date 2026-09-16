package workload

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes/fake"
)

func TestTrustedAdmissionPreservesTicketAndRejectsCallbackKeys(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "..")
	raw, err := os.ReadFile(filepath.Join(root, "deploy/k8s/base/runtime-controller/runtime-materialization-admission.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(raw), 64<<10)
	var policy admissionv1.ValidatingAdmissionPolicy
	for {
		policy = admissionv1.ValidatingAdmissionPolicy{}
		if err := decoder.Decode(&policy); err != nil {
			if errors.Is(err, io.EOF) {
				t.Fatal("runtime Pod policy missing")
			}
			t.Fatal(err)
		}
		if policy.Kind == "ValidatingAdmissionPolicy" && policy.Name == "runtime-role-pod-exact-secret-projection" {
			break
		}
	}
	encoded, err := json.Marshal([]admissionv1.ValidatingAdmissionPolicy{policy})
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "python3", "-B", filepath.Join(root, "tools/dev/trusted_cluster_render.py"), "materialize", "--profile", runtimecontract.CallbackProfileTrustedCluster)
	command.Stdin = bytes.NewReader(encoded)
	rendered, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	var policies []admissionv1.ValidatingAdmissionPolicy
	if json.Unmarshal(rendered, &policies) != nil || len(policies) != 1 {
		t.Fatal("trusted admission render invalid")
	}
	environment, err := cel.NewEnv(cel.Variable("object", cel.DynType), cel.Variable("variables", cel.DynType))
	if err != nil {
		t.Fatal(err)
	}
	var programs []cel.Program
	for _, validation := range policies[0].Spec.Validations {
		if validation.Message != "managed role Pod authority mount boundary is invalid" && validation.Message != "managed role Pod contains an unsupported runtime volume source" && validation.Message != "runtime Pod requires the configured trusted cluster profile" {
			continue
		}
		ast, issues := environment.Compile(validation.Expression)
		if issues != nil && issues.Err() != nil {
			t.Fatal(issues.Err())
		}
		program, err := environment.Program(ast, cel.CostLimit(100000))
		if err != nil {
			t.Fatal(err)
		}
		programs = append(programs, program)
	}
	if len(programs) != 3 {
		t.Fatal("trusted boundary expressions missing")
	}
	manager := newTestManager(t, fake.NewSimpleClientset())
	manager.config.RPCProfile = runtimecontract.CallbackProfileTrustedCluster
	input, binding, err := manager.BuildTurnInput(testExecution(false))
	if err != nil {
		t.Fatal(err)
	}
	credentials := testCredentialProjection(input)
	pod := manager.runtimePod(input, binding, &credentials, "runtime-ticket-fixture", "fixture", "turn")
	for name, mutate := range map[string]func(*corev1.Pod){
		"exact":           func(*corev1.Pod) {},
		"profile missing": func(p *corev1.Pod) { delete(p.Labels, "kodex.dev/security-profile") },
		"callback volume": func(p *corev1.Pod) { p.Spec.Volumes = append(p.Spec.Volumes, corev1.Volume{Name: "callback-ca"}) },
		"callback mount": func(p *corev1.Pod) {
			p.Spec.Containers[0].VolumeMounts = append(p.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{Name: "callback-ca", MountPath: "/unexpected"})
		},
		"missing ticket": func(p *corev1.Pod) { p.Spec.Containers[0].VolumeMounts = nil },
		"provider ticket": func(p *corev1.Pod) {
			p.Spec.Containers[1].VolumeMounts = append(p.Spec.Containers[1].VolumeMounts, corev1.VolumeMount{Name: "runtime-ticket", MountPath: "/ticket"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := pod.DeepCopy()
			mutate(candidate)
			object, err := k8sruntime.DefaultUnstructuredConverter.ToUnstructured(candidate)
			if err != nil {
				t.Fatal(err)
			}
			containers := object["spec"].(map[string]any)["containers"].([]any)
			activation := map[string]any{"object": object, "variables": map[string]any{
				"roleContainers": []any{containers[0]}, "providerContainers": []any{containers[1]}, "relayContainers": []any{containers[2]},
			}}
			accepted := true
			for _, program := range programs {
				result, _, err := program.Eval(activation)
				accepted = accepted && err == nil && result == types.True
			}
			if accepted != (name == "exact") {
				t.Fatal("trusted admission outcome mismatch")
			}
		})
	}
}
