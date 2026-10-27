package workload

import (
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	corev1 "k8s.io/api/core/v1"
)

const (
	// workspaceStorageProfileAnnotation retains V2 cleanup dependencies across controller replacement.
	// workspaceStorageProfileAnnotation сохраняет V2 cleanup dependencies после controller replacement.
	workspaceStorageProfileAnnotation = "runtime.kodex.dev/workspace-storage-profile"
	// workspaceStorageRefAnnotation pins the non-secret owner work identity for exact mount admission.
	// workspaceStorageRefAnnotation закрепляет non-secret owner work identity для exact mount admission.
	workspaceStorageRefAnnotation = "runtime.kodex.dev/workspace-storage-ref"
	// workspaceStorageGenerationAnnotation pins the owner generation; metadata is not a writer grant.
	// workspaceStorageGenerationAnnotation закрепляет owner generation; metadata не является writer grant.
	workspaceStorageGenerationAnnotation = "runtime.kodex.dev/workspace-storage-generation"
	// workspaceWriterFinalizer holds the exact owned Pod until positive quiescence is committed by its owner.
	// workspaceWriterFinalizer удерживает exact owned Pod до owner commit positive quiescence.
	workspaceWriterFinalizer = "runtime.kodex.dev/workspace-writer"
)

// configureWorkspaceStoragePod preserves legacy Pods and installs one exact V2 work mount after BIND.
// The first initializer alone receives the private PVC root; no writable metadata mount reaches models.
// configureWorkspaceStoragePod сохраняет legacy Pods и подключает exact V2 work mount после BIND.
// Только first initializer получает private PVC root; model не получает writable metadata mount.
func configureWorkspaceStoragePod(pod *corev1.Pod, input runtimecontract.RunnerInput) bool {
	if input.WorkspaceStorage == nil {
		return true
	}
	if pod == nil || input.WorkspaceStorage.ValidateForInput(input) != nil || len(pod.Spec.InitContainers) != 2 ||
		pod.Spec.InitContainers[0].Name != "workspace-prepare" || pod.Spec.InitContainers[1].Name != "workspace-init" {
		return false
	}
	tree, err := input.WorkspaceStorage.GenerationSubPath()
	if err != nil {
		return false
	}
	pod.Finalizers = append(pod.Finalizers, workspaceWriterFinalizer)
	if pod.Labels == nil {
		pod.Labels = make(map[string]string)
	}
	pod.Labels[workspaceStorageProfileAnnotation] = runtimecontract.WorkspaceStorageProfile
	metadata := strings.TrimSuffix(tree, "/tree")
	work := corev1.VolumeMount{Name: "session", MountPath: runtimecontract.RuntimeWorkspaceWorkingRoot, SubPath: tree}
	proof := corev1.VolumeMount{Name: "session", MountPath: runtimecontract.WorkspaceMountProofPath, SubPath: metadata + "/mount.json", ReadOnly: true}
	ack := corev1.VolumeMount{Name: "session", MountPath: runtimecontract.WorkspaceWriterAcknowledgementPath, SubPath: metadata + "/writer.json", ReadOnly: true}
	first := &pod.Spec.InitContainers[0]
	first.Args = []string{"runtime-bind-workspace"}
	first.WorkingDir = "/"
	first.TerminationMessagePath = "/dev/termination-log"
	first.TerminationMessagePolicy = corev1.TerminationMessageReadFile
	first.Env = []corev1.EnvVar{{Name: "KODEX_RUNTIME_REVISION_FILE", Value: "/var/run/config/kodex/runtime/runtime.json"}}
	first.VolumeMounts = []corev1.VolumeMount{
		{Name: "workspace", MountPath: "/workspace"},
		{Name: "session", MountPath: runtimecontract.WorkspaceStorageRootMount},
	}
	for _, mount := range pod.Spec.InitContainers[1].VolumeMounts {
		switch mount.Name {
		case "runtime-input", "runtime-ticket", "callback-ca", "callback-client":
			first.VolumeMounts = append(first.VolumeMounts, mount)
		}
	}
	pod.Spec.InitContainers[1].VolumeMounts = append(pod.Spec.InitContainers[1].VolumeMounts, work, proof, ack)
	for index := range pod.Spec.Containers {
		container := &pod.Spec.Containers[index]
		switch container.Name {
		case "role-runtime":
			container.TerminationMessagePath = "/dev/termination-log"
			container.TerminationMessagePolicy = corev1.TerminationMessageReadFile
			container.VolumeMounts = append(container.VolumeMounts, work, proof, ack)
		case "provider-runtime":
			container.VolumeMounts = append(container.VolumeMounts, work, proof)
		}
	}
	return true
}
