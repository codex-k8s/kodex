package workload

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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

// TestTrustedAdmissionPreservesTicketAndRejectsCallbackKeys renders the repository
// policy locally for the trusted profile and evaluates three selected CEL rules
// on ticket/profile/callback mutations. Eval errors count as rejection; the test
// neither installs admission policy nor proves a live trusted network boundary.
// TestTrustedAdmissionPreservesTicketAndRejectsCallbackKeys локально преобразует
// policy репозитория для trusted profile и проверяет три CEL-правила для мутаций
// ticket/profile/callback. Ошибка Eval считается отказом; admission policy
// не устанавливается и live trusted network boundary не доказывается.
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
			activation := runtimeAdmissionActivation(t, environment, policies[0].Spec.Variables, object)
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

// TestAdmissionMissingContainerMountDenial checks that a malformed sibling mount
// produces a false CEL result without an evaluation error when a container is absent.
// TestAdmissionMissingContainerMountDenial проверяет, что повреждённый mount
// соседнего контейнера при отсутствии другого даёт false без ошибки CEL.
func TestAdmissionMissingContainerMountDenial(t *testing.T) {
	program := runtimeAdmissionPrograms(t, "managed role Pod authority mount boundary is invalid")[0]
	manager := newTestManager(t, fake.NewSimpleClientset())
	input, binding, err := manager.BuildTurnInput(testExecution(false))
	if err != nil {
		t.Fatal(err)
	}
	credentials := testCredentialProjection(input)
	fixture := manager.runtimePod(input, binding, &credentials, "runtime-ticket-fixture", "fixture", "turn")
	for _, missing := range []string{"role-runtime", "provider-credential-relay"} {
		for _, writable := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/writable=%t", missing, writable), func(t *testing.T) {
				pod := fixture.DeepCopy()
				var containers []corev1.Container
				found := false
				for _, container := range pod.Spec.Containers {
					if container.Name == missing {
						continue
					}
					if container.Name == "role-runtime" || container.Name == "provider-credential-relay" {
						mounts := container.VolumeMounts[:0]
						for _, mount := range container.VolumeMounts {
							if mount.Name == "runtime-input" {
								found = true
								if !writable {
									continue
								}
								mount.ReadOnly = false
							}
							mounts = append(mounts, mount)
						}
						container.VolumeMounts = mounts
					}
					containers = append(containers, container)
				}
				if !found {
					t.Fatal("runtime input mount missing from fixture")
				}
				pod.Spec.Containers = containers
				object, err := k8sruntime.DefaultUnstructuredConverter.ToUnstructured(pod)
				if err != nil {
					t.Fatal(err)
				}
				spec := object["spec"].(map[string]any)
				all := spec["containers"].([]any)
				role, relay, provider := []any{}, []any{}, []any{}
				for _, item := range all {
					switch item.(map[string]any)["name"] {
					case "role-runtime":
						role = append(role, item)
					case "provider-credential-relay":
						relay = append(relay, item)
					case "provider-runtime":
						provider = append(provider, item)
					}
				}
				activation := map[string]any{"object": object, "variables": map[string]any{
					"roleContainers": role, "relayContainers": relay, "providerContainers": provider,
					"allContainers": append(append([]any{}, all...), spec["initContainers"].([]any)...),
				}}
				result, _, err := program.Eval(activation)
				if err != nil || result != types.False {
					t.Fatalf("expected false without CEL error, got %v: %v", result, err)
				}
			})
		}
	}
}
