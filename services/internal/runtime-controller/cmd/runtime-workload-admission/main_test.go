package main

import (
	"strings"
	"testing"

	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/enum"
	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
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
				InitContainers: []corev1.Container{{Image: "authority", SecurityContext: security.DeepCopy()}, {Image: roleImageRepository + "@sha256:" + strings.Repeat("a", 64), Args: []string{"runtime-init-workspace"}, SecurityContext: security.DeepCopy()}},
				Containers:     []corev1.Container{{Image: roleImageRepository + "@sha256:" + strings.Repeat("a", 64), Args: []string{"runtime-session"}, SecurityContext: security.DeepCopy()}, {Image: "authority", SecurityContext: security.DeepCopy()}},
			}
			clientgoscheme.Scheme.Default(pod)
			snapshot := runtimeSnapshot{Execution: execution, Revision: entity.Revision{ImageDigest: "sha256:" + strings.Repeat("a", 64)}, DesiredPod: pod.DeepCopy()}
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
		})
	}
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
