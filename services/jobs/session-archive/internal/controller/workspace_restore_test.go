package controller

import (
	"context"
	"encoding/json"
	"path"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/archive"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// workspaceRestoreTaskFixture selects an exact source checkpoint and later target generation without issuing a live owner grant.
// workspaceRestoreTaskFixture выбирает exact source checkpoint и later target generation, не выдавая live owner grant.
func workspaceRestoreTaskFixture(t *testing.T, reconcile bool) (model.Task, model.Result) {
	t.Helper()
	task := workspaceTaskFixture(t)
	result := model.Result{Success: true, FormatVersion: 2, ObjectKey: task.TargetObjectKey, ObjectVersion: "fixture-version",
		ObjectETag: "fixture-etag", ObjectDigest: "sha256:" + strings.Repeat("a", 64), ObjectSizeBytes: 1024,
		SourceSHA256: strings.Repeat("b", 64), SourceSizeBytes: 9, SourceEntryCount: 3}
	source, err := result.WorkspaceCheckpoint(task)
	if err != nil {
		t.Fatal("prepare exact source checkpoint")
	}
	task.Kind, task.TargetObjectVersion = "RESTORE", source.Manifest.Version
	task.Workspace.Quiescence, task.Workspace.Checkpoint = "", &source
	task.Workspace.Target.Generation++
	if reconcile {
		task.Attempt, task.Workspace.ReconcileOnly, task.InputDigest = 2, true, strings.Repeat("e", 64)
	}
	if task.Validate() != nil || !result.MatchesWorkspaceRestore(task) {
		t.Fatal("invalid restore render fixture")
	}
	return task, result
}

// TestWorkspaceRestoreRenderCannotFallBackToLegacy rejects incomplete fixed PVC bindings and malformed tasks before constructing a Pod.
// TestWorkspaceRestoreRenderCannotFallBackToLegacy отклоняет incomplete fixed PVC bindings и malformed tasks до создания Pod.
func TestWorkspaceRestoreRenderCannotFallBackToLegacy(t *testing.T) {
	for _, name := range []string{"missing pvc uid", "replaced pvc uid", "same generation", "foreign source", "unsupported kind"} {
		t.Run(name, func(t *testing.T) {
			task, _ := workspaceRestoreTaskFixture(t, false)
			uid := types.UID(task.Workspace.PVCUID)
			switch name {
			case "missing pvc uid":
				task.Workspace.PVCUID = ""
			case "replaced pvc uid":
				uid = "replacement"
			case "same generation":
				task.Workspace.Target.Generation = task.Workspace.Checkpoint.Generation
			case "foreign source":
				task.Workspace.Checkpoint.Scope.ProjectRef = "prj_foreign000"
			case "unsupported kind":
				task.Kind = "DELETE_OBJECT"
			}
			controller, _ := New(newArchiveClient(), testConfig())
			if controller.job(workloadName(task.TaskRef, task.ContentGeneration, task.Attempt), task, uid) != nil ||
				controller.validWorkspaceWorkerPod(task, nil, nil) {
				t.Fatal("invalid workspace restore reached legacy geometry")
			}
		})
	}
}

// TestWorkspaceRestoreJobSeparatesInitializationWritingAndReconciliation checks exact mounts, identities and unchanged combined resource limits.
// TestWorkspaceRestoreJobSeparatesInitializationWritingAndReconciliation проверяет exact mounts/identities и прежние combined resource limits.
func TestWorkspaceRestoreJobSeparatesInitializationWritingAndReconciliation(t *testing.T) {
	for _, reconcile := range []bool{false, true} {
		name := "fresh"
		if reconcile {
			name = "readonly rejoin"
		}
		t.Run(name, func(t *testing.T) {
			task, _ := workspaceRestoreTaskFixture(t, reconcile)
			client := newArchiveClient()
			controller, _ := New(client, testConfig())
			job := controller.job(workloadName(task.TaskRef, task.ContentGeneration, task.Attempt), task, types.UID(task.Workspace.PVCUID))
			pod := &corev1.Pod{Spec: *job.Spec.Template.Spec.DeepCopy()}
			if job.Spec.TTLSecondsAfterFinished != nil || pod.Spec.SecurityContext.FSGroup != nil ||
				!controller.validWorkspaceWorkerPod(task, job, pod) || *pod.Spec.Containers[0].SecurityContext.RunAsUser != 10001 ||
				*pod.Spec.Containers[0].SecurityContext.RunAsGroup != 29000 {
				t.Fatal("restore main identity or evidence retention changed")
			}
			subpath, _ := task.Workspace.Target.GenerationSubPath()
			dataSubpath := path.Join(path.Dir(subpath), ".restore-tree")
			if reconcile {
				dataSubpath = subpath
			}
			var memory, cpu int64
			for _, container := range pod.Spec.Containers {
				memory += container.Resources.Limits.Memory().Value()
				cpu += container.Resources.Limits.Cpu().MilliValue()
				data, metadata := false, false
				for _, mount := range container.VolumeMounts {
					if mount.Name != "session" {
						continue
					}
					switch mount.MountPath {
					case archive.WorkspaceRestoreRoot:
						data = mount.SubPath == dataSubpath && mount.ReadOnly == reconcile
					case archive.WorkspaceRestoreGenerationMount:
						metadata = container.Name == "worker" && mount.SubPath == path.Dir(subpath) && mount.ReadOnly == reconcile
					default:
						t.Fatal("restore consumer received full PVC or foreign generation")
					}
				}
				if !data || container.Name == "worker" && !metadata || container.Name != "worker" && metadata {
					t.Fatal("restore consumer data or metadata geometry changed")
				}
			}
			if reconcile {
				if len(pod.Spec.Containers) != 1 || len(pod.Spec.InitContainers) != 0 || memory != 224<<20 || cpu != 900 {
					t.Fatal("read-only rejoin gained an initializer or provider")
				}
				for _, mount := range pod.Spec.Containers[0].VolumeMounts {
					if mount.MountPath == archive.WorkspaceWriterSocketDirectory {
						t.Fatal("read-only rejoin gained a writer socket")
					}
				}
			} else {
				if len(pod.Spec.Containers) != 2 || len(pod.Spec.InitContainers) != 2 || memory != 256<<20 || cpu != 1000 ||
					*pod.Spec.Containers[1].SecurityContext.RunAsUser != 10002 || *pod.Spec.Containers[1].SecurityContext.RunAsGroup != 29000 {
					t.Fatal("restore helper identities or combined limits expanded")
				}
				for _, mount := range pod.Spec.Containers[1].VolumeMounts {
					if mount.Name == "object-storage" || mount.MountPath == archive.WorkspaceRestoreGenerationMount || mount.MountPath == "/tmp" {
						t.Fatal("provider received credentials, metadata or tmp root")
					}
				}
				initialize, prepare := pod.Spec.InitContainers[0], pod.Spec.InitContainers[1]
				if len(initialize.VolumeMounts) != 2 || initialize.VolumeMounts[1].MountPath != runtimecontract.WorkspaceStorageRootMount ||
					initialize.VolumeMounts[1].SubPath != "" || *initialize.SecurityContext.RunAsUser != 10001 ||
					len(prepare.VolumeMounts) != 1 || prepare.VolumeMounts[0].MountPath != "/tmp" || *prepare.SecurityContext.RunAsUser != 10001 {
					t.Fatal("initializer and socket preparation privileges crossed")
				}
			}
			for label, change := range map[string]func(*corev1.Pod){
				"recursive ownership":    func(p *corev1.Pod) { p.Spec.SecurityContext.FSGroup = int64Ptr(29000) },
				"parent data mount":      func(p *corev1.Pod) { p.Spec.Containers[0].VolumeMounts[3].SubPath = "" },
				"mount write mode":       func(p *corev1.Pod) { p.Spec.Containers[0].VolumeMounts[3].ReadOnly = !reconcile },
				"alternate source":       func(p *corev1.Pod) { p.Spec.Volumes[3].PersistentVolumeClaim.ClaimName = "foreign" },
				"alternate uid":          func(p *corev1.Pod) { p.Spec.Containers[0].SecurityContext.RunAsUser = int64Ptr(10002) },
				"additional credentials": func(p *corev1.Pod) { p.Spec.Containers[0].EnvFrom = []corev1.EnvFromSource{{Prefix: "FOREIGN"}} },
				"unplanned helper":       func(p *corev1.Pod) { p.Spec.Containers = append(p.Spec.Containers, p.Spec.Containers[0]) },
			} {
				t.Run(label, func(t *testing.T) {
					changed := pod.DeepCopy()
					change(changed)
					if controller.validWorkspaceWorkerPod(task, job, changed) {
						t.Fatal("changed restore consumer authority accepted")
					}
				})
			}
			if _, _, err := controller.ExecuteRetained(t.Context(), task, func(context.Context) error {
				t.Fatal("dormant restore geometry renewed authority")
				return nil
			}); err == nil || len(client.Actions()) != 0 {
				t.Fatal("restore render enabled an incomplete owner lifecycle")
			}
		})
	}
}

// TestWorkspaceSuccessRequiresAllPhysicalConsumersAndFrozenResult exercises actual readResult against complete and inconsistent API observations.
// The fake API proves interpretation only, not a Kubernetes deployment or owner RESTORED transition.
// TestWorkspaceSuccessRequiresAllPhysicalConsumersAndFrozenResult проверяет actual readResult на complete/inconsistent API observations.
// Fake API доказывает interpretation, не Kubernetes deployment либо owner RESTORED transition.
func TestWorkspaceSuccessRequiresAllPhysicalConsumersAndFrozenResult(t *testing.T) {
	for _, profile := range []string{"snapshot", "restore", "reconcile"} {
		t.Run(profile, func(t *testing.T) {
			task, result := workspaceRestoreTaskFixture(t, profile == "reconcile")
			if profile == "snapshot" {
				task = workspaceTaskFixture(t)
			}
			controller, _ := New(newArchiveClient(), testConfig())
			job := controller.job(workloadName(task.TaskRef, task.ContentGeneration, task.Attempt), task, types.UID(task.Workspace.PVCUID))
			job.UID, job.Status.Succeeded = "worker-job-uid", 1
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: job.Name + "-fixture", UID: "worker-pod-uid", Namespace: exactWorkerNamespace,
				Labels: map[string]string{"job-name": job.Name}, OwnerReferences: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: job.Name, UID: job.UID, Controller: boolPtr(true)}}},
				Spec: *job.Spec.Template.Spec.DeepCopy(), Status: corev1.PodStatus{Phase: corev1.PodSucceeded}}
			for _, group := range []struct {
				containers []corev1.Container
				statuses   *[]corev1.ContainerStatus
			}{{pod.Spec.Containers, &pod.Status.ContainerStatuses}, {pod.Spec.InitContainers, &pod.Status.InitContainerStatuses}} {
				for _, container := range group.containers {
					*group.statuses = append(*group.statuses, corev1.ContainerStatus{Name: container.Name,
						State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}})
				}
			}
			raw, _ := json.Marshal(result)
			pod.Status.ContainerStatuses[0].State.Terminated.Message = string(raw)
			cases := map[string]func(*corev1.Pod){
				"complete":         func(*corev1.Pod) {},
				"pod not terminal": func(p *corev1.Pod) { p.Status.Phase = corev1.PodRunning },
				"missing consumer": func(p *corev1.Pod) {
					p.Status.ContainerStatuses = p.Status.ContainerStatuses[:len(p.Status.ContainerStatuses)-1]
				},
				"duplicate consumer": func(p *corev1.Pod) {
					p.Status.ContainerStatuses = append(p.Status.ContainerStatuses, p.Status.ContainerStatuses[0])
				},
				"consumer still running": func(p *corev1.Pod) {
					p.Status.ContainerStatuses[len(p.Status.ContainerStatuses)-1].State = corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
				},
				"consumer failed": func(p *corev1.Pod) {
					p.Status.ContainerStatuses[len(p.Status.ContainerStatuses)-1].State.Terminated.ExitCode = 1
				},
				"consumer restarted": func(p *corev1.Pod) { p.Status.ContainerStatuses[0].RestartCount = 1 },
				"foreign status":     func(p *corev1.Pod) { p.Status.ContainerStatuses[len(p.Status.ContainerStatuses)-1].Name = "foreign" },
				"metadata mismatch": func(p *corev1.Pod) {
					changed := result
					changed.ObjectVersion += "-other"
					if profile == "snapshot" {
						changed.ObjectKey += "-other"
					}
					raw, _ := json.Marshal(changed)
					p.Status.ContainerStatuses[0].State.Terminated.Message = string(raw)
				},
			}
			if len(pod.Spec.InitContainers) > 0 {
				cases["missing initializer"] = func(p *corev1.Pod) { p.Status.InitContainerStatuses = nil }
				cases["failed initializer"] = func(p *corev1.Pod) { p.Status.InitContainerStatuses[0].State.Terminated.ExitCode = 1 }
			}
			if profile != "snapshot" {
				for label, mutate := range map[string]func(*model.Result){
					"failed metadata":   func(r *model.Result) { r.Success = false },
					"safe error":        func(r *model.Result) { r.SafeErrorCode = "SESSION_ARCHIVE_WORKER_FAILED" },
					"failure stage":     func(r *model.Result) { r.FailureStage = model.FailureStageUnknown },
					"wrong format":      func(r *model.Result) { r.FormatVersion = 1 },
					"wrong root":        func(r *model.Result) { r.ObjectKey += "-other" },
					"wrong etag":        func(r *model.Result) { r.ObjectETag += "-other" },
					"wrong digest":      func(r *model.Result) { r.ObjectDigest = "sha256:" + strings.Repeat("c", 64) },
					"wrong root size":   func(r *model.Result) { r.ObjectSizeBytes++ },
					"wrong tree":        func(r *model.Result) { r.SourceSHA256 = strings.Repeat("c", 64) },
					"wrong tree size":   func(r *model.Result) { r.SourceSizeBytes++ },
					"wrong entry count": func(r *model.Result) { r.SourceEntryCount++ },
				} {
					cases[label] = func(p *corev1.Pod) {
						changed := result
						mutate(&changed)
						raw, _ := json.Marshal(changed)
						p.Status.ContainerStatuses[0].State.Terminated.Message = string(raw)
					}
				}
			}
			for name, change := range cases {
				t.Run(name, func(t *testing.T) {
					changed := pod.DeepCopy()
					change(changed)
					controller.client = newArchiveClient(changed)
					actual, err := controller.readResult(t.Context(), task, job, types.UID(task.Workspace.PVCUID))
					if name == "complete" && (err != nil || actual != result) || name != "complete" && err == nil {
						t.Fatal("physical join or frozen result boundary changed")
					}
				})
			}
		})
	}
}
