package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestEnsureRestorePVCCreatesBoundedCanonicalVolume(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset()
	controller, err := New(client, testConfig())
	if err != nil {
		t.Fatalf("create controller: %v", err)
	}
	task := model.Task{PVCName: "runtime-session-0123456789abcdef", InputDigest: strings.Repeat("a", 64)}
	if err := controller.ensureRestorePVC(context.Background(), task); err != nil {
		t.Fatalf("ensure restore PVC: %v", err)
	}
	pvc, err := client.CoreV1().PersistentVolumeClaims("kodex-system").Get(context.Background(), task.PVCName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("read restore PVC: %v", err)
	}
	if pvc.Annotations[restoreInputAnnotation] != task.InputDigest || pvc.Spec.Resources.Requests.Storage().String() != "20Gi" {
		t.Fatalf("restore PVC is not bound to the immutable task: %#v", pvc)
	}

	conflict := task
	conflict.InputDigest = strings.Repeat("b", 64)
	if err := controller.ensureRestorePVC(context.Background(), conflict); err == nil {
		t.Fatal("controller accepted a PVC from another restore input")
	}
}

func TestWorkerJobUsesSessionVolumeGroupWithoutServiceAccountToken(t *testing.T) {
	t.Parallel()

	controller, err := New(fake.NewSimpleClientset(), testConfig())
	if err != nil {
		t.Fatalf("create controller: %v", err)
	}
	job := controller.job("session-archive-test", model.Task{Kind: "SNAPSHOT", PVCName: "runtime-session-0123456789abcdef"})
	pod := job.Spec.Template.Spec
	if pod.SecurityContext == nil || pod.SecurityContext.FSGroup == nil || *pod.SecurityContext.FSGroup != 29000 {
		t.Fatalf("worker cannot read runner-owned session files: %#v", pod.SecurityContext)
	}
	if pod.AutomountServiceAccountToken == nil || *pod.AutomountServiceAccountToken {
		t.Fatal("worker received a Kubernetes service account token")
	}
}

func testConfig() Config {
	return Config{Namespace: "kodex-system", Environment: "staging", WorkerImage: "example.invalid/session-archive@sha256:" + strings.Repeat("0", 64),
		WorkerServiceAccount: "session-archive-worker", SessionPVCSize: "20Gi", ObjectStorageSecret: "object-storage",
		ObjectStorageEndpoint: "https://s3.example.invalid", ObjectStorageRegion: "us-east-1", ObjectStorageBucket: "archives",
		WorkerTimeout: 8 * time.Minute}
}
