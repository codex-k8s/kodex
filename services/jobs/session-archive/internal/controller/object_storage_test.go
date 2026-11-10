package controller

import (
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// TestStorageCAIsOptionalAndRestrictedToStorageWorker covers legacy and V2 jobs without granting helpers access to credentials or CA mounts.
// RU: TestStorageCAIsOptionalAndRestrictedToStorageWorker проверяет legacy и V2 jobs без передачи credentials или CA mounts помощникам.
func TestStorageCAIsOptionalAndRestrictedToStorageWorker(t *testing.T) {
	t.Parallel()
	snapshot := workspaceTaskFixture(t)
	restore, _ := workspaceRestoreTaskFixture(t, false)
	for _, task := range []model.Task{
		{Kind: "SNAPSHOT", PVCName: "runtime-session-fixture000"},
		{Kind: "RESTORE", PVCName: "runtime-session-fixture000"},
		{Kind: "DELETE_OBJECT"}, snapshot, restore,
	} {
		for _, enabled := range []bool{false, true} {
			config := testConfig()
			if enabled {
				config.ObjectStorageCAConfigMap = "synthetic-storage-ca"
			}
			controller, err := New(newArchiveClient(), config)
			if err != nil {
				t.Fatal(err)
			}
			uid := types.UID("synthetic-pvc")
			if task.Workspace != nil {
				uid = types.UID(task.Workspace.PVCUID)
			}
			job := controller.job("synthetic-archive", task, uid)
			if job == nil {
				t.Fatal("valid archive job rejected")
			}
			pod := job.Spec.Template.Spec
			found := false
			for _, volume := range pod.Volumes {
				if volume.Name == "object-storage-ca" {
					found = true
					if volume.ConfigMap == nil || volume.ConfigMap.Name != config.ObjectStorageCAConfigMap ||
						len(volume.ConfigMap.Items) != 1 || volume.ConfigMap.Items[0].Key != "ca.crt" || volume.ConfigMap.Items[0].Path != "ca.crt" ||
						volume.ConfigMap.Optional != nil && *volume.ConfigMap.Optional {
						t.Fatal("storage CA volume has unexpected source or optional mount")
					}
				}
			}
			if found != enabled {
				t.Fatal("storage CA did not preserve optional profile")
			}
			for _, container := range append(pod.Containers, pod.InitContainers...) {
				mount, environment := false, false
				for _, item := range container.VolumeMounts {
					if item.Name == "object-storage-ca" {
						mount = true
						if !item.ReadOnly || item.MountPath != objectStorageCAMountPath {
							t.Fatal("storage CA mount is mutable or misplaced")
						}
					}
				}
				for _, item := range container.Env {
					if item.Name == "SSL_CERT_FILE" {
						environment = true
						if item.ValueFrom != nil || item.Value != objectStorageCAMountPath+"/ca.crt" {
							t.Fatal("storage CA environment is not the fixed file")
						}
					}
				}
				want := enabled && container.Name == "worker"
				if mount != want || environment != want {
					t.Fatal("storage CA leaked to a helper or did not reach the storage worker")
				}
			}
			if task.Workspace != nil && !controller.validWorkspaceWorkerPod(task, job, &corev1.Pod{Spec: pod}) {
				t.Fatal("configured CA broke exact workspace worker readback")
			}
		}
	}
}

// TestStorageCARejectsNonConfigMapReferences keeps the optional reference inside the fixed worker namespace.
// RU: TestStorageCARejectsNonConfigMapReferences сохраняет optional reference в фиксированном namespace worker.
func TestStorageCARejectsNonConfigMapReferences(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"../ca", "other/ca", "/ca", "ca cert", "CA", strings.Repeat("a", 254)} {
		config := testConfig()
		config.ObjectStorageCAConfigMap = name
		if _, err := New(newArchiveClient(), config); err == nil {
			t.Fatal("invalid storage CA reference accepted")
		}
	}
}
