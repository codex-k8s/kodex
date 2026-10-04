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
				for _, container := range pod.Spec.Containers[:2] {
					for _, mount := range container.VolumeMounts {
						if mount.Name == volumeName {
							mounts = append(mounts, mount)
						}
					}
				}
				if len(mounts) != 2 || !reflect.DeepEqual(mounts[0], mounts[1]) || mounts[0].ReadOnly || mounts[0].MountPath != "/workspace/.kodex/volumes/"+name {
					t.Fatal("executor and driver do not share exact workspace volumes")
				}
				for _, container := range append(slices.Clone(pod.Spec.InitContainers), pod.Spec.Containers[2]) {
					if hasMount(container, volumeName) {
						t.Fatal("environment volume reached an authority or init container")
					}
				}
			}
			if hasMount(pod.Spec.Containers[0], "provider-auth") || hasMount(pod.Spec.Containers[1], "runtime-ticket") || hasMount(pod.Spec.Containers[1], "callback-client") {
				t.Fatal("volume change expanded credential authority")
			}
		})
	}
}

func TestEnvironmentVolumeAdmissionRejectsAsymmetricAndAuthorityMounts(t *testing.T) {
	programs := runtimeAdmissionPrograms(t, "runtime environment volume mount path is outside the closed workspace catalog", "runtime environment volumes must have exact shared driver and executor mounts")
	pod := executorEnvironmentPod(t, "turn")
	volumeName := "environment-" + shortHash("scratch")
	for name, mutate := range map[string]func(*corev1.Pod){
		"exact": func(*corev1.Pod) {},
		"missing executor": func(p *corev1.Pod) {
			p.Spec.Containers[1].VolumeMounts = slices.DeleteFunc(p.Spec.Containers[1].VolumeMounts, func(m corev1.VolumeMount) bool { return m.Name == volumeName })
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
