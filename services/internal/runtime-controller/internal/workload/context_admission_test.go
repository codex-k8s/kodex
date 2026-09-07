package workload

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes/fake"
)

func TestContextMountAdmissionEvaluatesGeneratedPodAndRejectsDrift(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "deploy", "k8s", "base", "runtime-controller", "runtime-materialization-admission.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(raw), 64<<10)
	var expressions []string
	for {
		var policy admissionv1.ValidatingAdmissionPolicy
		err := decoder.Decode(&policy)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, validation := range policy.Spec.Validations {
			if validation.Message == "workspace preparation boundary is invalid" ||
				validation.Message == "managed role Pod container set is invalid" ||
				validation.Message == "runtime context cannot have aliases or nested mounts" ||
				strings.HasPrefix(strings.TrimSpace(validation.Expression), "variables.providerContainers[0].volumeMounts.all") {
				expressions = append(expressions, validation.Expression)
			}
		}
	}
	if len(expressions) != 4 {
		t.Fatalf("expected four workspace admission expressions, got %d", len(expressions))
	}
	env, err := cel.NewEnv(cel.Variable("object", cel.DynType), cel.Variable("variables", cel.DynType))
	if err != nil {
		t.Fatal(err)
	}
	programs := make([]cel.Program, 0, len(expressions))
	for _, expression := range expressions {
		ast, issues := env.Compile(expression)
		if issues != nil && issues.Err() != nil {
			t.Fatal(issues.Err())
		}
		program, err := env.Program(ast, cel.CostLimit(100000))
		if err != nil {
			t.Fatal(err)
		}
		programs = append(programs, program)
	}
	manager := newTestManager(t, fake.NewSimpleClientset())
	input, binding, err := manager.BuildTurnInput(testExecution(false))
	if err != nil {
		t.Fatal(err)
	}
	credentials := testCredentialProjection(input)
	pod := manager.runtimePod(input, binding, &credentials, "runtime-ticket-fixture", "fixture", "turn")
	for name, mutate := range map[string]func(*corev1.Pod){
		"exact":      func(*corev1.Pod) {},
		"warm exact": func(*corev1.Pod) {},
		"reversed init order": func(p *corev1.Pod) {
			p.Spec.InitContainers[0], p.Spec.InitContainers[1] = p.Spec.InitContainers[1], p.Spec.InitContainers[0]
		},
		"prepare nested mount": func(p *corev1.Pod) {
			p.Spec.InitContainers[0].VolumeMounts = append(p.Spec.InitContainers[0].VolumeMounts, corev1.VolumeMount{Name: "session", MountPath: "/workspace/.kodex/state"})
		},
		"prepare credential": func(p *corev1.Pod) {
			p.Spec.InitContainers[0].Env = []corev1.EnvVar{{Name: "SECRET", Value: "synthetic"}}
		},
		"prepare root": func(p *corev1.Pod) {
			p.Spec.InitContainers[0].SecurityContext.RunAsUser = int64Pointer(0)
		},
		"prepare wrong process": func(p *corev1.Pod) {
			p.Spec.InitContainers[0].Args = []string{"runtime-init-workspace"}
		},
		"prepare sidecar": func(p *corev1.Pod) {
			value := corev1.ContainerRestartPolicyAlways
			p.Spec.InitContainers[0].RestartPolicy = &value
		},
		"provider writable": func(p *corev1.Pod) {
			for i := range p.Spec.Containers[1].VolumeMounts {
				if p.Spec.Containers[1].VolumeMounts[i].Name == "runtime-context" {
					p.Spec.Containers[1].VolumeMounts[i].ReadOnly = false
				}
			}
		},
		"runner writable": func(p *corev1.Pod) {
			for i := range p.Spec.Containers[0].VolumeMounts {
				if p.Spec.Containers[0].VolumeMounts[i].Name == "runtime-context" {
					p.Spec.Containers[0].VolumeMounts[i].ReadOnly = false
				}
			}
		},
		"init readonly": func(p *corev1.Pod) {
			for i := range p.Spec.InitContainers[1].VolumeMounts {
				if p.Spec.InitContainers[1].VolumeMounts[i].Name == "runtime-context" {
					p.Spec.InitContainers[1].VolumeMounts[i].ReadOnly = true
				}
			}
		},
		"subpath": func(p *corev1.Pod) {
			for i := range p.Spec.Containers[1].VolumeMounts {
				if p.Spec.Containers[1].VolumeMounts[i].Name == "runtime-context" {
					p.Spec.Containers[1].VolumeMounts[i].SubPath = "other"
				}
			}
		},
		"alias": func(p *corev1.Pod) {
			p.Spec.Containers[1].VolumeMounts = append(p.Spec.Containers[1].VolumeMounts, corev1.VolumeMount{Name: "runtime-context", MountPath: "/context-alias"})
		},
		"nested": func(p *corev1.Pod) {
			p.Spec.Containers[1].VolumeMounts = append(p.Spec.Containers[1].VolumeMounts, corev1.VolumeMount{Name: "workspace", MountPath: "/workspace/context/skills"})
		},
		"relay access": func(p *corev1.Pod) {
			p.Spec.Containers[2].VolumeMounts = append(p.Spec.Containers[2].VolumeMounts, corev1.VolumeMount{Name: "runtime-context", MountPath: "/workspace/context", ReadOnly: true})
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := pod.DeepCopy()
			if name == "warm exact" {
				warm, warmBinding, err := manager.BuildWarmInput(testWarmRevision())
				if err != nil {
					t.Fatal(err)
				}
				candidate = manager.runtimePod(warm, warmBinding, nil, "runtime-ticket-fixture", "fixture", "warm")
			}
			mutate(candidate)
			object, err := k8sruntime.DefaultUnstructuredConverter.ToUnstructured(candidate)
			if err != nil {
				t.Fatal(err)
			}
			spec := object["spec"].(map[string]any)
			containers, init := spec["containers"].([]any), spec["initContainers"].([]any)
			activation := map[string]any{"object": object, "variables": map[string]any{
				"allContainers": append(append([]any{}, init...), containers...), "roleContainers": []any{containers[0]},
				"providerContainers": []any{containers[1]}, "relayContainers": []any{containers[2]},
			}}
			accepted := true
			for _, program := range programs {
				result, _, err := program.Eval(activation)
				if err != nil {
					t.Fatal(err)
				}
				accepted = accepted && result == types.True
			}
			if accepted != (name == "exact" || name == "warm exact") {
				t.Fatal("context admission outcome is invalid")
			}
		})
	}
}
