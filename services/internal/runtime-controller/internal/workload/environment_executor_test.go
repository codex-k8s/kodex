package workload

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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

// executorEnvironmentPod builds a sealed synthetic warm or turn input with
// normalized disk/memory volumes and returns its generated Pod specification.
// Values other than warm take the turn-input branch; this helper is not a mode
// validator and neither creates a Pod nor runs an executor.
// executorEnvironmentPod собирает закреплённый synthetic warm либо turn input
// с нормализованными disk/memory volumes и возвращает спецификацию Pod.
// Значения кроме warm выбирают turn-ветку; helper не проверяет mode,
// не создаёт Pod и не запускает executor.
func executorEnvironmentPod(t *testing.T, mode string) *corev1.Pod {
	t.Helper()
	manager := newTestManager(t, fake.NewSimpleClientset())
	execution := testExecution(mode == "warm")
	if mode == "warm" {
		execution.Revision = testWarmRevision()
	}
	policy := runtimecontract.DefaultRuntimeEnvironmentPolicy()
	policy.Volumes = []runtimecontract.RuntimeVolume{{Name: "scratch", Kind: runtimecontract.RuntimeVolumeEphemeralDisk, SizeMiB: 64}, {Name: "cache", Kind: runtimecontract.RuntimeVolumeEphemeralMemory, SizeMiB: 32}}
	policy, err := runtimecontract.NormalizeRuntimeEnvironmentPolicy(policy)
	if err != nil {
		t.Fatal(err)
	}
	execution.Revision.EnvironmentPolicy = testRuntimeEnvironmentPolicyProto(policy)
	image, tools := runtimeEnvironmentContract(execution.Revision)
	execution.Revision.RuntimeEnvironmentDigest, err = runtimecontract.RuntimeEnvironmentDigest(nil, nil, image, tools, policy)
	if err != nil {
		t.Fatal(err)
	}
	var input runtimecontract.RunnerInput
	var binding ProviderSecretBinding
	if mode == "warm" {
		sealTestWarmRevision(execution.Revision)
		input, binding, err = manager.BuildWarmInput(execution.Revision)
		if err != nil {
			t.Fatal(err)
		}
	} else {
		sealTestTurnExecution(execution)
		input, binding, err = manager.BuildTurnInput(execution)
		if err != nil {
			t.Fatal(err)
		}
	}
	credentials := testCredentialProjection(input)
	return manager.runtimePod(input, binding, &credentials, "runtime-ticket-fixture", "fixture", mode)
}

// TestRuntimeExecutorUsesConfiguredResourcesAndSharedEnvironmentVolumes checks
// provider-only configured resources, smaller driver/relay resources, exact shared
// writable initializer/role/provider mounts and unchanged credential separation
// on generated warm/turn Pod specifications, not live resource enforcement.
// TestRuntimeExecutorUsesConfiguredResourcesAndSharedEnvironmentVolumes проверяет
// configured resources лишь провайдера, меньшие ресурсы driver/relay, точные общие
// writable mounts initializer/role/provider и разделение реквизитов в спецификациях
// warm/turn Pod, не реальное применение resource limits.
func TestRuntimeExecutorUsesConfiguredResourcesAndSharedEnvironmentVolumes(t *testing.T) {
	for _, mode := range []string{"turn", "warm"} {
		t.Run(mode, func(t *testing.T) {
			pod := executorEnvironmentPod(t, mode)
			if !reflect.DeepEqual(pod.Spec.Containers[1].Resources, runtimePolicyResourceRequirements(runtimecontract.DefaultRuntimeEnvironmentPolicy().Resources)) ||
				!reflect.DeepEqual(pod.Spec.Containers[0].Resources, smallResources()) || !reflect.DeepEqual(pod.Spec.Containers[2].Resources, smallResources()) {
				t.Fatal("configured resources were doubled or assigned to the driver")
			}
			for _, name := range []string{"scratch", "cache"} {
				volumeName := "environment-" + shortHash(name)
				var mounts []corev1.VolumeMount
				for _, container := range append(slices.Clone(pod.Spec.Containers[:2]), pod.Spec.InitContainers[1]) {
					for _, mount := range container.VolumeMounts {
						if mount.Name == volumeName {
							mounts = append(mounts, mount)
						}
					}
				}
				if len(mounts) != 3 || !reflect.DeepEqual(mounts[0], mounts[1]) || !reflect.DeepEqual(mounts[0], mounts[2]) || mounts[0].ReadOnly || mounts[0].MountPath != "/workspace/.kodex/volumes/"+name {
					t.Fatal("initializer executor and driver do not share exact workspace volumes")
				}
				for _, container := range []corev1.Container{pod.Spec.InitContainers[0], pod.Spec.Containers[2]} {
					if hasMount(container, volumeName) {
						t.Fatal("environment volume reached an unrelated authority or preparation container")
					}
				}
			}
			if hasMount(pod.Spec.Containers[0], "provider-auth") || hasMount(pod.Spec.Containers[1], "runtime-ticket") || hasMount(pod.Spec.Containers[1], "callback-client") {
				t.Fatal("volume change expanded credential authority")
			}
		})
	}
}

// TestEnvironmentVolumeAdmissionRejectsAsymmetricAndAuthorityMounts evaluates
// selected repository CEL rules against exact mounts and missing, aliased,
// asymmetric, read-only or authority-container mutations of one generated Pod.
// TestEnvironmentVolumeAdmissionRejectsAsymmetricAndAuthorityMounts проверяет
// выбранные CEL-правила репозитория для точных mounts и мутаций отсутствия,
// alias, асимметрии, read-only и передачи authority-контейнерам одного Pod.
func TestEnvironmentVolumeAdmissionRejectsAsymmetricAndAuthorityMounts(t *testing.T) {
	programs := runtimeAdmissionPrograms(t, "runtime environment volume mount path is outside the closed workspace catalog", "runtime environment volumes must have exact shared initializer driver and executor mounts")
	pod := executorEnvironmentPod(t, "turn")
	volumeName := "environment-" + shortHash("scratch")
	for name, mutate := range map[string]func(*corev1.Pod){
		"exact": func(*corev1.Pod) {},
		"missing executor": func(p *corev1.Pod) {
			p.Spec.Containers[1].VolumeMounts = slices.DeleteFunc(p.Spec.Containers[1].VolumeMounts, func(m corev1.VolumeMount) bool { return m.Name == volumeName })
		},
		"missing initializer": func(p *corev1.Pod) {
			p.Spec.InitContainers[1].VolumeMounts = slices.DeleteFunc(p.Spec.InitContainers[1].VolumeMounts, func(m corev1.VolumeMount) bool { return m.Name == volumeName })
		},
		"preparer": func(p *corev1.Pod) {
			p.Spec.InitContainers[0].VolumeMounts = append(p.Spec.InitContainers[0].VolumeMounts, corev1.VolumeMount{Name: volumeName, MountPath: "/workspace/.kodex/volumes/scratch"})
		},
		"initializer readonly": func(p *corev1.Pod) {
			for i := range p.Spec.InitContainers[1].VolumeMounts {
				if p.Spec.InitContainers[1].VolumeMounts[i].Name == volumeName {
					p.Spec.InitContainers[1].VolumeMounts[i].ReadOnly = true
				}
			}
		},
		"initializer different path": func(p *corev1.Pod) {
			for i := range p.Spec.InitContainers[1].VolumeMounts {
				if p.Spec.InitContainers[1].VolumeMounts[i].Name == volumeName {
					p.Spec.InitContainers[1].VolumeMounts[i].MountPath = "/workspace/.kodex/volumes/foreign"
				}
			}
		},
		"alias": func(p *corev1.Pod) {
			p.Spec.Containers[1].VolumeMounts = append(p.Spec.Containers[1].VolumeMounts, corev1.VolumeMount{Name: volumeName, MountPath: "/workspace/.kodex/volumes/alias"})
		},
		"relay": func(p *corev1.Pod) {
			p.Spec.Containers[2].VolumeMounts = append(p.Spec.Containers[2].VolumeMounts, corev1.VolumeMount{Name: volumeName, MountPath: "/workspace/.kodex/volumes/scratch"})
		},
		"init": func(p *corev1.Pod) {
			p.Spec.InitContainers[1].VolumeMounts = append(p.Spec.InitContainers[1].VolumeMounts, corev1.VolumeMount{Name: volumeName, MountPath: "/workspace/.kodex/volumes/scratch"})
		},
		"subpath": func(p *corev1.Pod) {
			for i := range p.Spec.Containers[1].VolumeMounts {
				if p.Spec.Containers[1].VolumeMounts[i].Name == volumeName {
					p.Spec.Containers[1].VolumeMounts[i].SubPath = "child"
				}
			}
		},
		"different path": func(p *corev1.Pod) {
			for i := range p.Spec.Containers[1].VolumeMounts {
				if p.Spec.Containers[1].VolumeMounts[i].Name == volumeName {
					p.Spec.Containers[1].VolumeMounts[i].MountPath = "/workspace/.kodex/volumes/foreign"
				}
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := pod.DeepCopy()
			mutate(candidate)
			assertRuntimeAdmission(t, programs, candidate, name == "exact")
		})
	}
}

// TestRuntimeTransportAdmissionRejectsUnsafeOverrides checks selected CEL rules
// on foreign CA/proxy bypass, duplicate proxy and unsupported TLS/proxy env
// mutations; local expression results are not a deployed egress-policy proof.
// TestRuntimeTransportAdmissionRejectsUnsafeOverrides проверяет выбранные CEL
// для чужого CA, обхода proxy, его дубля и запрещённых TLS/proxy env;
// локальный результат выражений не доказывает работу развёрнутой egress policy.
func TestRuntimeTransportAdmissionRejectsUnsafeOverrides(t *testing.T) {
	programs := runtimeAdmissionPrograms(t, "provider runtime server-owned environment is invalid", "provider runtime contains an unsupported environment variable")
	for _, name := range []string{"exact", "foreignCA", "foreignBypass", "duplicateProxy", "GIT_SSL_NO_VERIFY", "NODE_TLS_REJECT_UNAUTHORIZED", "PYTHONHTTPSVERIFY", "CURL_SSL_BACKEND", "ALL_PROXY"} {
		t.Run(name, func(t *testing.T) {
			candidate := executorEnvironmentPod(t, "turn")
			env := &candidate.Spec.Containers[1].Env
			switch name {
			case "exact":
			case "foreignCA", "foreignBypass":
				key := "SSL_CERT_FILE"
				value := "/tmp/foreign"
				if name == "foreignBypass" {
					key = "NO_PROXY"
					value = "*"
				}
				for i := range *env {
					if (*env)[i].Name == key {
						(*env)[i].Value = value
					}
				}
			case "duplicateProxy":
				*env = append(*env, corev1.EnvVar{Name: "HTTPS_PROXY", Value: "http://foreign"})
			default:
				*env = append(*env, corev1.EnvVar{Name: name, Value: "unsafe"})
			}
			assertRuntimeAdmission(t, programs, candidate, name == "exact")
		})
	}
}

// runtimeAdmissionPrograms reads repository admission YAML and compiles only
// validations with the requested messages using object/variables and cost 100000.
// It requires matching program/message counts, not uniqueness or full policy
// installation, and fails the test on YAML/compile/program errors.
// runtimeAdmissionPrograms читает admission YAML репозитория и компилирует только
// проверки с заданными messages с object/variables и cost 100000. Требует равенства
// числа programs и messages, не уникальности и не установки всей policy;
// ошибка YAML, compile или Program завершает тест.
func runtimeAdmissionPrograms(t *testing.T, messages ...string) []cel.Program {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "deploy/k8s/base/runtime-controller/runtime-materialization-admission.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(raw), 64<<10)
	var programs []cel.Program
	environment, err := cel.NewEnv(cel.Variable("object", cel.DynType), cel.Variable("variables", cel.DynType))
	if err != nil {
		t.Fatal(err)
	}
	for {
		var policy admissionv1.ValidatingAdmissionPolicy
		if err := decoder.Decode(&policy); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		for _, validation := range policy.Spec.Validations {
			if !slices.Contains(messages, validation.Message) {
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
	}
	if len(programs) != len(messages) {
		t.Fatal("runtime admission expressions are missing")
	}
	return programs
}

// assertRuntimeAdmission projects a Pod into local CEL object/role/provider/all
// bindings and compares combined results with the expected decision. Eval errors
// count as rejection, so a negative case alone does not prove error-free execution
// or actual API-server admission behavior.
// assertRuntimeAdmission переводит Pod в локальные CEL object/role/provider/all
// и сравнивает совокупный результат с ожидаемым. Ошибка Eval считается отказом,
// поэтому один негативный кейс не доказывает отсутствие ошибок вычисления
// и не подтверждает реальный admission API server.
func assertRuntimeAdmission(t *testing.T, programs []cel.Program, candidate *corev1.Pod, expected bool) {
	t.Helper()
	object, err := k8sruntime.DefaultUnstructuredConverter.ToUnstructured(candidate)
	if err != nil {
		t.Fatal(err)
	}
	spec := object["spec"].(map[string]any)
	containers := spec["containers"].([]any)
	activation := map[string]any{"object": object, "variables": map[string]any{"roleContainers": []any{containers[0]}, "providerContainers": []any{containers[1]}, "allContainers": append(slices.Clone(containers), spec["initContainers"].([]any)...)}}
	accepted := true
	for _, program := range programs {
		result, _, err := program.Eval(activation)
		accepted = accepted && err == nil && result == types.True
	}
	if accepted != expected {
		t.Fatal("runtime admission did not reject drift")
	}
}
