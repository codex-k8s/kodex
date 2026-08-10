package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/enum"
	"github.com/google/uuid"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

func TestExactRuntimePodAccessProfileAdmissionMatrix(t *testing.T) {
	for _, profile := range []enum.AccessProfile{enum.AccessNone} {
		t.Run(string(profile), func(t *testing.T) {
			execution := entity.Execution{
				ID: uuid.NewString(), OrganizationID: uuid.NewString(), ProjectID: uuid.NewString(),
				SessionID: uuid.NewString(), RoleID: uuid.NewString(), ThreadID: "thread-1",
				AccessProfile: profile, RuntimeRevisionSHA256: strings.Repeat("1", 64),
				ImmutableInputSHA256: strings.Repeat("2", 64), CredentialSnapshotSHA256: strings.Repeat("3", 64),
			}
			account := "runtime-access-" + stableHash(execution.OrganizationID+":"+execution.ProjectID+":"+
				execution.SessionID+":"+execution.RoleID+":"+execution.ID, 24)
			security := &corev1.SecurityContext{
				RunAsNonRoot: boolPointer(true), AllowPrivilegeEscalation: boolPointer(false),
				ReadOnlyRootFilesystem: boolPointer(true),
				Capabilities:           &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
			}
			roleSecurity := security.DeepCopy()
			roleSecurity.RunAsUser = testInt64Pointer(10001)
			providerSecurity := security.DeepCopy()
			providerSecurity.RunAsUser = testInt64Pointer(10002)
			issuerSecurity := security.DeepCopy()
			issuerSecurity.RunAsUser = testInt64Pointer(29001)
			pod := &corev1.Pod{}
			pod.Name = "runtime-role-" + stableHash(execution.RoleID+":"+execution.ThreadID+":"+execution.SessionID+":"+execution.ID, 24)
			pod.Namespace = admissionNamespace
			pod.Labels = map[string]string{"app.kubernetes.io/component": "role-runtime", "runtime.mattercodex.dev/access-profile": strings.ToLower(string(profile))}
			pod.Annotations = map[string]string{
				"runtime.mattercodex.dev/execution-id":               execution.ID,
				"runtime.mattercodex.dev/revision-sha256":            execution.RuntimeRevisionSHA256,
				"runtime.mattercodex.dev/input-sha256":               execution.ImmutableInputSHA256,
				"runtime.mattercodex.dev/credential-snapshot-sha256": execution.CredentialSnapshotSHA256,
			}
			pod.Spec = corev1.PodSpec{
				ServiceAccountName: account, AutomountServiceAccountToken: boolPointer(false), RestartPolicy: corev1.RestartPolicyNever,
				InitContainers: []corev1.Container{
					{Name: "internal-rpc-authority-socket-init", Image: "authority", SecurityContext: security.DeepCopy()},
					{Name: "workspace-init", Image: "registry.example.test/agent-runner@sha256:" + strings.Repeat("a", 64), Args: []string{"runtime-init-workspace"}, SecurityContext: roleSecurity.DeepCopy()},
				},
				Containers: []corev1.Container{
					{Name: "role-runtime", Image: "registry.example.test/agent-runner@sha256:" + strings.Repeat("a", 64), Args: []string{"runtime-session"}, SecurityContext: roleSecurity.DeepCopy()},
					{Name: "provider-runtime", Image: "registry.example.test/agent-runner@sha256:" + strings.Repeat("a", 64), Args: []string{"runtime-provider"}, SecurityContext: providerSecurity.DeepCopy(), VolumeMounts: []corev1.VolumeMount{{Name: "session"}, {Name: "provider-socket"}, {Name: "provider-tmp"}}},
					{Name: "internal-rpc-authority-issuer", Image: "authority", Command: []string{"/usr/local/bin/internal-rpc-authority-issuer"}, SecurityContext: issuerSecurity.DeepCopy()},
				},
			}
			clientgoscheme.Scheme.Default(pod)
			snapshot := runtimeSnapshot{Execution: execution, Revision: entity.Revision{ImageReference: "registry.example.test/agent-runner@sha256:" + strings.Repeat("a", 64)}, DesiredPod: pod.DeepCopy()}
			if !exactRuntimePod("system:serviceaccount:mattercodex-system:runtime-workload-materializer", pod, snapshot) {
				t.Fatal("exact access profile was denied")
			}
			if exactRuntimePod("system:serviceaccount:mattercodex-system:runtime-credential-broker", pod, snapshot) {
				t.Fatal("wrong issuer was admitted")
			}
			changed := pod.DeepCopy()
			delete(changed.Labels, "runtime.mattercodex.dev/access-profile")
			if exactRuntimePod("system:serviceaccount:mattercodex-system:runtime-workload-materializer", changed, snapshot) {
				t.Fatal("missing profile label was admitted")
			}
			mutations := map[string]func(*corev1.Pod){
				"provider uid": func(changed *corev1.Pod) { *changed.Spec.Containers[1].SecurityContext.RunAsUser = 10001 },
				"provider authority mount": func(changed *corev1.Pod) {
					changed.Spec.Containers[1].VolumeMounts = append(changed.Spec.Containers[1].VolumeMounts, corev1.VolumeMount{Name: "authority-sockets"})
				},
				"provider service account token": func(changed *corev1.Pod) {
					changed.Spec.Containers[1].VolumeMounts = append(changed.Spec.Containers[1].VolumeMounts, corev1.VolumeMount{Name: "kube-api-access"})
				},
				"missing issuer": func(changed *corev1.Pod) { changed.Spec.Containers = changed.Spec.Containers[:2] },
				"role command":   func(changed *corev1.Pod) { changed.Spec.Containers[0].Args = []string{"runtime-provider"} },
			}
			for name, mutate := range mutations {
				t.Run(name, func(t *testing.T) {
					changed := pod.DeepCopy()
					mutate(changed)
					if exactRuntimePod("system:serviceaccount:mattercodex-system:runtime-workload-materializer", changed, snapshot) {
						t.Fatal("mutated runtime Pod was admitted")
					}
				})
			}
		})
	}
}

func testInt64Pointer(value int64) *int64 { return &value }

func TestDisabledArchiveRestoreCapabilityRejectsDynamicResources(t *testing.T) {
	handler := &admissionHandler{archiveRestoreEnabled: false}
	for _, object := range []struct {
		resource string
		value    any
	}{
		{resource: "pods", value: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Namespace: admissionNamespace,
			Labels:    map[string]string{"app.kubernetes.io/component": "runtime-s3-credential-readback"},
		}}},
		{resource: "secrets", value: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			Namespace: admissionNamespace,
			Labels:    map[string]string{"app.kubernetes.io/component": "runtime-s3-credential"},
		}}},
	} {
		raw, err := json.Marshal(object.value)
		if err != nil {
			t.Fatal(err)
		}
		allowed, message := handler.admit(context.Background(), &admissionv1.AdmissionRequest{
			UID: "disabled-capability", Operation: admissionv1.Create, Namespace: admissionNamespace,
			Resource: metav1.GroupVersionResource{Version: "v1", Resource: object.resource},
			Object:   runtimeRawExtension(raw),
		})
		if allowed || message != "runtime archive/restore capability is disabled" {
			t.Fatalf("disabled %s resource was not rejected: allowed=%v message=%q", object.resource, allowed, message)
		}
	}
}

func TestArchiveRestoreCapabilityProfile(t *testing.T) {
	t.Setenv("RUNTIME_ARCHIVE_RESTORE_CAPABILITY", "disabled")
	enabled, err := archiveRestoreCapability()
	if err != nil || enabled {
		t.Fatal("disabled archive/restore capability was not selected")
	}
	t.Setenv("RUNTIME_ARCHIVE_RESTORE_CAPABILITY", "unexpected")
	if _, err := archiveRestoreCapability(); err == nil {
		t.Fatal("unknown archive/restore capability was accepted")
	}
}

func runtimeRawExtension(raw []byte) runtime.RawExtension {
	return runtime.RawExtension{Raw: raw}
}

func TestExactS3ReadbackPodAdmissionMatrix(t *testing.T) {
	t.Setenv("RUNTIME_S3_READBACK_IMAGE", "registry.invalid/runtime-controller@sha256:"+strings.Repeat("a", 64))
	execution := entity.Execution{
		ID: uuid.NewString(), OrganizationID: uuid.NewString(), ProjectID: uuid.NewString(),
		SessionID: uuid.NewString(), RoleID: uuid.NewString(), ThreadID: "thread-1",
		AccessProfile: enum.AccessNone, RuntimeRevisionSHA256: strings.Repeat("1", 64),
		ImmutableInputSHA256: strings.Repeat("2", 64), CredentialSnapshotSHA256: strings.Repeat("3", 64),
	}
	action := "archive"
	snapshot := runtimeSnapshot{Execution: execution, ArchiveWorkloadTicket: "archive-ticket"}
	pod := desiredS3ReadbackPod(snapshot, action, strings.Repeat("b", 64))
	clientgoscheme.Scheme.Default(pod)
	issuer := "system:serviceaccount:mattercodex-system:runtime-s3-archive-exchanger"
	if !exactS3ReadbackPod(issuer, pod, snapshot) {
		t.Fatal("exact archive readback Pod was denied")
	}
	if exactS3ReadbackPod("system:serviceaccount:mattercodex-system:runtime-s3-restore-exchanger", pod, snapshot) {
		t.Fatal("wrong action issuer was admitted")
	}
	changed := pod.DeepCopy()
	changed.Spec.ServiceAccountName = "runtime-role-cluster-admin"
	if exactS3ReadbackPod(issuer, changed, snapshot) {
		t.Fatal("changed readback service account was admitted")
	}
}
