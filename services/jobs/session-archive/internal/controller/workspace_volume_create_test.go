package controller

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

// workspaceVolumeCreateFixture freezes a first physical intent without a model lease, provider
// account or arbitrary PVC locator; the real contract supplies its canonical input digest.
// workspaceVolumeCreateFixture фиксирует first physical intent без model lease, provider account
// либо произвольного PVC locator; настоящий контракт назначает canonical input digest.
func workspaceVolumeCreateFixture() runtimecontract.WorkspaceVolumeWork {
	work := runtimecontract.WorkspaceVolumeWork{TaskRef: "svt_create0000", Kind: runtimecontract.WorkspaceVolumeCreate, Attempt: 1,
		Volume: runtimecontract.WorkspaceVolumeInput{Profile: runtimecontract.WorkspaceVolumeLocalPathProfile,
			Scope:        runtimecontract.WorkspaceStorageScope{OrganizationRef: "org_fixture000", ProjectRef: "prj_fixture000", SessionRef: "ses_fixture000", AgentRef: "agt_fixture000"},
			WorkspaceRef: "wks_fixture000", VolumeGeneration: 1, CreateTaskRef: "svt_create0000"}}
	work.InputDigest, _ = work.Digest()
	return work
}

// workspaceVolumeCreateControllerFixture selects the explicit qualified storage class instead
// of relying on a cluster default; fake API actions remain inspectable without a live cluster.
// workspaceVolumeCreateControllerFixture выбирает explicit qualified storage class вместо cluster
// default; fake API actions доступны для проверки без живого кластера.
func workspaceVolumeCreateControllerFixture(t *testing.T) (*fake.Clientset, *Controller) {
	t.Helper()
	client := fake.NewSimpleClientset()
	config := testConfig()
	config.StorageClass = "local-path"
	controller, err := New(client, config)
	if err != nil {
		t.Fatal("controller fixture rejected")
	}
	return client, controller
}

// TestWorkspaceVolumeCreateKeepsPositiveUID proves the first owner-renewed API response is
// retained exactly and no read, adoption, worker Job or cleanup occurs before owner acceptance.
// TestWorkspaceVolumeCreateKeepsPositiveUID доказывает точное сохранение first owner-renewed API
// response без read, adoption, worker Job либо cleanup до принятия owner.
func TestWorkspaceVolumeCreateKeepsPositiveUID(t *testing.T) {
	client, controller := workspaceVolumeCreateControllerFixture(t)
	const uid = "10000000-0000-4000-8000-000000000001"
	client.PrependReactor("create", "persistentvolumeclaims", func(action ktesting.Action) (bool, runtime.Object, error) {
		pvc := action.(ktesting.CreateAction).GetObject().(*corev1.PersistentVolumeClaim).DeepCopy()
		pvc.UID = uid
		if err := client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("persistentvolumeclaims"), pvc, exactWorkerNamespace); err != nil {
			return true, nil, err
		}
		return true, pvc, nil
	})
	renewed := 0
	observed, err := controller.CreateWorkspaceVolume(t.Context(), workspaceVolumeCreateFixture(), func(context.Context) error {
		renewed++
		return nil
	})
	actions := client.Actions()
	if err != nil || observed != uid || renewed != 1 || len(actions) != 1 || !actions[0].Matches("create", "persistentvolumeclaims") {
		t.Fatal("positive Create was lost, repeated or followed by adoption/cleanup")
	}
	pvc := actions[0].(ktesting.CreateAction).GetObject().(*corev1.PersistentVolumeClaim)
	if pvc.Spec.Resources.Requests.Storage().Value() != runtimecontract.WorkspaceVolumeRequestedBytes ||
		pvc.Spec.DataSource != nil || pvc.Spec.DataSourceRef != nil || pvc.Spec.VolumeName != "" || len(pvc.OwnerReferences) != 0 {
		t.Fatal("Create expanded the qualified storage request")
	}
	if _, err := client.Tracker().Get(corev1.SchemeGroupVersion.WithResource("persistentvolumeclaims"), exactWorkerNamespace, pvc.Name); err != nil {
		t.Fatal("positive unaccepted PVC was removed")
	}
}

// TestWorkspaceVolumeCreateNeverAdoptsUnknownResults rejects AlreadyExists, uncertain transport
// and malformed positive responses without issuing a read, retry or delete against the volume.
// TestWorkspaceVolumeCreateNeverAdoptsUnknownResults отклоняет AlreadyExists, uncertain transport
// и malformed positive responses без read, retry либо delete тома.
func TestWorkspaceVolumeCreateNeverAdoptsUnknownResults(t *testing.T) {
	for name, response := range map[string]func(*corev1.PersistentVolumeClaim) (runtime.Object, error){
		"already-exists": func(pvc *corev1.PersistentVolumeClaim) (runtime.Object, error) {
			return nil, apierrors.NewAlreadyExists(schema.GroupResource{Resource: "persistentvolumeclaims"}, pvc.Name)
		},
		"transport-unknown": func(*corev1.PersistentVolumeClaim) (runtime.Object, error) {
			return nil, errors.New("fixture transport uncertainty")
		},
		"missing-uid": func(pvc *corev1.PersistentVolumeClaim) (runtime.Object, error) { return pvc, nil },
		"changed-owner": func(pvc *corev1.PersistentVolumeClaim) (runtime.Object, error) {
			pvc.UID = "10000000-0000-4000-8000-000000000001"
			pvc.Annotations["workspace.kodex.dev/workspace-ref"] = "wks_foreign000"
			return pvc, nil
		},
		"deleting": func(pvc *corev1.PersistentVolumeClaim) (runtime.Object, error) {
			pvc.UID = "10000000-0000-4000-8000-000000000001"
			now := metav1.Now()
			pvc.DeletionTimestamp = &now
			return pvc, nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			client, controller := workspaceVolumeCreateControllerFixture(t)
			client.PrependReactor("create", "persistentvolumeclaims", func(action ktesting.Action) (bool, runtime.Object, error) {
				result, err := response(action.(ktesting.CreateAction).GetObject().(*corev1.PersistentVolumeClaim).DeepCopy())
				return true, result, err
			})
			uid, err := controller.CreateWorkspaceVolume(t.Context(), workspaceVolumeCreateFixture(), func(context.Context) error { return nil })
			actions := client.Actions()
			if !errors.Is(err, errWorkspaceVolumeCreateUnconfirmed) || uid != "" || len(actions) != 1 || !actions[0].Matches("create", "persistentvolumeclaims") {
				t.Fatal("uncertain response caused adoption, retry, deletion or fabricated ownership")
			}
		})
	}
}

// TestWorkspaceVolumeCreateRequiresFirstFreshAuthority keeps reconciliation, unsupported
// storage and denied owner renewal outside the Kubernetes effect boundary.
// TestWorkspaceVolumeCreateRequiresFirstFreshAuthority оставляет reconciliation, unsupported
// storage и denied owner renewal вне границы Kubernetes effect.
func TestWorkspaceVolumeCreateRequiresFirstFreshAuthority(t *testing.T) {
	for _, attempt := range []int32{2, 3, 4, 5} {
		client, controller := workspaceVolumeCreateControllerFixture(t)
		work := workspaceVolumeCreateFixture()
		work.Attempt, work.ReconcileOnly = attempt, true
		work.InputDigest, _ = work.Digest()
		if uid, err := controller.CreateWorkspaceVolume(t.Context(), work, func(context.Context) error { return nil }); !errors.Is(err, errWorkspaceVolumeCreateUnconfirmed) || uid != "" || len(client.Actions()) != 0 {
			t.Fatal("reconciliation acquired another physical Create")
		}
	}
	client, controller := workspaceVolumeCreateControllerFixture(t)
	if uid, err := controller.CreateWorkspaceVolume(t.Context(), workspaceVolumeCreateFixture(), func(context.Context) error {
		return errors.New("fixture owner denial")
	}); !errors.Is(err, errWorkspaceVolumeCreateUnconfirmed) || uid != "" || len(client.Actions()) != 0 {
		t.Fatal("denied owner grant reached Kubernetes")
	}
	controller.config.StorageClass = "foreign-class"
	if uid, err := controller.CreateWorkspaceVolume(t.Context(), workspaceVolumeCreateFixture(), func(context.Context) error { return nil }); !errors.Is(err, errWorkspaceVolumeCreateUnconfirmed) || uid != "" || len(client.Actions()) != 0 {
		t.Fatal("unsupported storage profile reached Kubernetes")
	}
}
