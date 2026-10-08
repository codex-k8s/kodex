package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestArchiveAPIFailureIsClosedAndExact(t *testing.T) {
	for _, scenario := range []struct {
		name, verb, resource, stage, reason string
		code                                int32
	}{
		{"input already exists", "create", "configmaps", "CREATE_INPUT", "AlreadyExists", 409},
		{"job forbidden", "create", "jobs", "CREATE_JOB", "Forbidden", 403},
		{"job invalid", "create", "jobs", "CREATE_JOB", "Invalid", 422},
		{"unknown reason", "create", "jobs", "CREATE_JOB", "SENTINEL_REASON", 999},
		{"job read timeout", "get", "jobs", "OBSERVE_JOB", "ServerTimeout", 504},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			task := testSnapshotTask()
			task.OrganizationRef, task.SessionRef, task.ProviderAccountRef, task.RuntimeRevisionRef = "org_fixture01", "ses_fixture01", "pva_fixture01", "rrev_fixture01"
			task.RuntimeRevisionVersion = 1
			task.RuntimeRevisionDigest, task.InputDigest, task.SourceSHA256 = strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
			task.CodexSessionID = "00000000-0000-4000-8000-000000000003"
			task.SourceRelativePath = ".kodex/state/codex-home/sessions/2026/08/28/rollout-2026-08-28T23-23-39-" + task.CodexSessionID + ".jsonl"
			task.SourceSizeBytes, task.TargetObjectKey = 1024, "SENTINEL_PRIVATE_OBJECT"
			task.PVCName, _ = runtimecontract.SessionPVCName(task.SessionRef)
			client := fake.NewSimpleClientset(&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: task.PVCName, Namespace: exactWorkerNamespace, UID: "source-pvc-uid"}})
			client.PrependReactor(scenario.verb, scenario.resource, func(clienttesting.Action) (bool, runtime.Object, error) {
				return true, nil, &apierrors.StatusError{ErrStatus: metav1.Status{Reason: metav1.StatusReason(scenario.reason), Code: scenario.code,
					Message: "SENTINEL_PRIVATE_MESSAGE", Details: &metav1.StatusDetails{Name: "SENTINEL_PRIVATE_RESOURCE", Causes: []metav1.StatusCause{{Message: "SENTINEL_PRIVATE_CAUSE"}}}}}
			})
			var logs bytes.Buffer
			cfg := testConfig()
			cfg.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
			controller, err := New(client, cfg)
			if err != nil {
				t.Fatal(err)
			}
			controller.poll = time.Millisecond
			result, err := controller.Execute(t.Context(), task, func(context.Context) error { return nil })
			if err == nil || result.Success {
				t.Fatal("API failure changed canonical business result")
			}
			var record map[string]any
			if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			wantReason, wantCode := scenario.reason, scenario.code
			if scenario.name == "unknown reason" {
				wantReason, wantCode = "UNKNOWN", 0
			}
			if record["msg"] != archiveAPIFailureMessage || record[archiveAPIStageAttribute] != scenario.stage ||
				record[archiveAPIReasonAttribute] != wantReason || record[archiveAPIStatusAttribute] != float64(wantCode) ||
				record[archiveTaskRefAttribute] != task.TaskRef || record[archiveSessionRefAttribute] != task.SessionRef ||
				strings.Contains(logs.String(), "SENTINEL") || len(record) != 11 {
				t.Fatal("API failure diagnostic is missing, unbounded or exposed private data")
			}
		})
	}
	for _, item := range []struct {
		err    error
		reason string
	}{
		{fmt.Errorf("SENTINEL_PRIVATE: %w", context.DeadlineExceeded), "CLIENT_TIMEOUT"},
		{fmt.Errorf("SENTINEL_PRIVATE: %w", context.Canceled), "CLIENT_CANCELLED"},
		{fmt.Errorf("SENTINEL_PRIVATE"), "UNKNOWN"},
	} {
		if reason, code := boundedAPIError(item.err); reason != item.reason || code != 0 {
			t.Fatal("untyped error escaped closed classification")
		}
	}
}

func TestWorkerTerminalDiagnosticIsBoundedAndExact(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"failure", "failure before cleanup", "source identity", "source digest", "object write", "object readback", "unknown failure stage", "success", "exit zero malformed", "status conflict", "foreign pod", "forged task", "forged attempt", "replaced pvc", "unknown reason", "missing result"} {
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			cfg := testConfig()
			cfg.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
			task := testSnapshotTask()
			task.OrganizationRef, task.SessionRef = "org_fixture01", "ses_fixture01"
			task.ProviderAccountRef, task.RuntimeRevisionRef, task.RuntimeRevisionVersion = "pva_fixture01", "rrev_fixture01", 1
			task.RuntimeRevisionDigest, task.InputDigest, task.SourceSHA256 = strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
			task.CodexSessionID = "00000000-0000-4000-8000-000000000003"
			task.SourceRelativePath = ".kodex/state/codex-home/sessions/2026/08/28/rollout-2026-08-28T23-23-39-" + task.CodexSessionID + ".jsonl"
			task.SourceSizeBytes, task.TargetObjectKey = 1024, "fixture/object.tar"
			task.PVCName, _ = runtimecontract.SessionPVCName(task.SessionRef)
			controller, err := New(fake.NewSimpleClientset(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			uid := types.UID("source-pvc-uid")
			job := controller.job(workloadName(task.TaskRef, task.ContentGeneration, task.Attempt), task, uid)
			job.UID = "worker-job-uid"
			job.Status.Failed = 1
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: job.Name + "-fixture", UID: "worker-pod-uid", Namespace: exactWorkerNamespace,
				Labels: map[string]string{"job-name": job.Name}, OwnerReferences: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: job.Name, UID: job.UID, Controller: boolPtr(true)}}},
				Spec:   job.Spec.Template.Spec,
				Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "worker", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, Reason: "Error", Message: `{"success":false,"safe_error_code":"SESSION_ARCHIVE_WORKER_FAILED","object_key":"private sentinel"}`}}}}}}
			wantStage, wantCode, wantReason, wantErr := "WORKER_REPORTED_FAILURE", "SESSION_ARCHIVE_WORKER_FAILED", "Error", false
			wantFailureStage := "UNKNOWN"
			if name == "foreign pod" || strings.HasPrefix(name, "forged") || name == "replaced pvc" {
				pod.Status.ContainerStatuses[0].State.Terminated.Message = `{"success":false,"safe_error_code":"SESSION_ARCHIVE_WORKER_FAILED","failure_stage":"SOURCE_DIGEST"}`
			}
			switch name {
			case "failure before cleanup":
				wantFailureStage = "SOURCE_DIGEST"
				pod.Status.ContainerStatuses[0].State.Terminated.Message = `{"success":false,"safe_error_code":"SESSION_ARCHIVE_WORKER_FAILED","failure_stage":"SOURCE_DIGEST"}`
			case "source identity", "source digest", "object write", "object readback":
				wantFailureStage = strings.ToUpper(strings.ReplaceAll(name, " ", "_"))
				pod.Status.ContainerStatuses[0].State.Terminated.Message = fmt.Sprintf(`{"success":false,"safe_error_code":"SESSION_ARCHIVE_WORKER_FAILED","failure_stage":%q,"object_key":"private sentinel"}`, wantFailureStage)
			case "unknown failure stage":
				pod.Status.ContainerStatuses[0].State.Terminated.Message = `{"success":false,"safe_error_code":"SESSION_ARCHIVE_WORKER_FAILED","failure_stage":"private sentinel"}`
			case "success":
				job.Status.Failed, job.Status.Succeeded = 0, 1
				pod.Status.ContainerStatuses[0].State.Terminated.Message = `{"success":true,"failure_stage":"SOURCE_IDENTITY"}`
				pod.Status.ContainerStatuses[0].State.Terminated.ExitCode = 0
				wantStage, wantCode = "WORKER_REPORTED_SUCCESS", "NONE"
			case "exit zero malformed":
				pod.Status.ContainerStatuses[0].State.Terminated.Message = "private sentinel"
				pod.Status.ContainerStatuses[0].State.Terminated.ExitCode = 0
				wantStage, wantCode, wantErr = "RESULT_DECODE_INVALID", "UNKNOWN", true
			case "status conflict":
				pod.Status.ContainerStatuses[0].State.Terminated.Message = `{"success":true,"failure_stage":"SOURCE_IDENTITY"}`
				wantStage, wantCode, wantErr = "RESULT_STATUS_CONFLICT", "NONE", true
			case "foreign pod":
				pod.OwnerReferences[0].UID = "foreign-job-uid"
				wantStage, wantCode, wantReason, wantErr = "POD_BINDING_INVALID", "UNKNOWN", "UNKNOWN", true
			case "forged task":
				job.Annotations[taskRefAnnotation] = "sat_foreign01"
				wantStage, wantCode, wantReason, wantErr = "JOB_BINDING_INVALID", "UNKNOWN", "UNKNOWN", true
			case "forged attempt":
				job.Annotations[taskAttemptAnnotation] = "5"
				wantStage, wantCode, wantReason, wantErr = "JOB_BINDING_INVALID", "UNKNOWN", "UNKNOWN", true
			case "replaced pvc":
				job.Annotations[sourcePVCUIDAnnotation] = "foreign-pvc-uid"
				wantStage, wantCode, wantReason, wantErr = "JOB_BINDING_INVALID", "UNKNOWN", "UNKNOWN", true
			case "unknown reason":
				pod.Status.ContainerStatuses[0].State.Terminated.Reason = "private sentinel"
				pod.Status.ContainerStatuses[0].State.Terminated.Message = `{"success":false,"safe_error_code":"private sentinel"}`
				wantCode, wantReason = "UNKNOWN", "UNKNOWN"
			case "missing result":
				pod.Status.ContainerStatuses = nil
				wantStage, wantCode, wantReason, wantErr = "RESULT_MISSING", "UNKNOWN", "UNKNOWN", true
			}
			controller.client = fake.NewSimpleClientset(pod)
			var result model.Result
			if name == "failure before cleanup" {
				client := fake.NewSimpleClientset(pod, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: task.PVCName, Namespace: exactWorkerNamespace, UID: uid}})
				client.PrependReactor("get", "jobs", func(action clienttesting.Action) (bool, runtime.Object, error) {
					if _, err := client.Tracker().Get(action.GetResource(), action.GetNamespace(), action.(clienttesting.GetAction).GetName()); err != nil {
						return false, nil, nil
					}
					return true, job.DeepCopy(), nil
				})
				client.PrependReactor("delete", "jobs", func(clienttesting.Action) (bool, runtime.Object, error) {
					if !strings.Contains(logs.String(), `"failure_stage":"SOURCE_DIGEST"`) {
						t.Fatal("worker failure stage was lost before cleanup")
					}
					return false, nil, nil
				})
				controller.client, controller.poll = client, time.Millisecond
				result, err = controller.Execute(t.Context(), task, func(context.Context) error { return nil })
				assertWorkerResourcesRemoved(t, client, job.Name)
			} else {
				result, err = controller.readResult(t.Context(), task, job, uid)
			}
			if strings.HasPrefix(name, "forged") || name == "replaced pvc" {
				if len(controller.client.(*fake.Clientset).Actions()) != 0 {
					t.Fatal("forged job reached private result read")
				}
			}
			if (err != nil) != wantErr {
				t.Fatal("terminal outcome changed")
			}
			if name == "failure before cleanup" && (result.Success || result.SafeErrorCode != wantCode) {
				t.Fatal("diagnostics changed the canonical worker failure")
			}
			var record map[string]any
			if json.Unmarshal(logs.Bytes(), &record) != nil {
				t.Fatal("closed worker observation missing")
			}
			if record["msg"] != workerObservationMessage || record["stage"] != wantStage || record["safe_error_code"] != wantCode || record["termination_reason"] != wantReason || record[archiveFailureStageAttribute] != wantFailureStage || record["task_ref"] != task.TaskRef || record["session_ref"] != task.SessionRef || strings.Contains(logs.String(), "private sentinel") {
				t.Fatal("closed diagnostic binding or redaction failed")
			}
		})
	}
}
