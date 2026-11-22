package kubernetestarget

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	appsv1 "k8s.io/api/apps/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// storageEvidenceFixture models exact noncredential CSI application data, not a real cluster.
// storageEvidenceFixture моделирует exact noncredential CSI app data, не реальный кластер.
func storageEvidenceFixture() (managedkubernetes.StorageQualification, corev1.PersistentVolumeClaim, corev1.PersistentVolume, storagev1.StorageClass, storagev1.CSIDriver) {
	className := "app-data"
	claim := corev1.PersistentVolumeClaim{TypeMeta: metav1.TypeMeta{Kind: "PersistentVolumeClaim", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "example", UID: types.UID("11111111-1111-4111-8111-111111111111")},
		Spec:       corev1.PersistentVolumeClaimSpec{StorageClassName: &className, VolumeName: "app-volume"}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}
	volume := corev1.PersistentVolume{TypeMeta: metav1.TypeMeta{Kind: "PersistentVolume", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{Name: "app-volume", UID: types.UID("22222222-2222-4222-8222-222222222222")},
		Spec: corev1.PersistentVolumeSpec{StorageClassName: className, ClaimRef: &corev1.ObjectReference{Name: claim.Name, Namespace: claim.Namespace, UID: claim.UID},
			PersistentVolumeSource: corev1.PersistentVolumeSource{CSI: &corev1.CSIPersistentVolumeSource{Driver: "csi.example", VolumeHandle: "synthetic-app-data"}}},
		Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeBound}}
	class := storagev1.StorageClass{TypeMeta: metav1.TypeMeta{Kind: "StorageClass", APIVersion: "storage.k8s.io/v1"},
		ObjectMeta: metav1.ObjectMeta{Name: className, UID: types.UID("33333333-3333-4333-8333-333333333333")}, Provisioner: "csi.example"}
	driver := storagev1.CSIDriver{TypeMeta: metav1.TypeMeta{Kind: "CSIDriver", APIVersion: "storage.k8s.io/v1"},
		ObjectMeta: metav1.ObjectMeta{Name: "csi.example", UID: types.UID("44444444-4444-4444-8444-444444444444")}}
	profile := managedkubernetes.StorageQualification{Provider: managedkubernetes.StorageProviderCSIAppDataV1,
		PVCNamespace: claim.Namespace, PVCName: claim.Name, PVCUID: string(claim.UID), PVName: volume.Name, PVUID: string(volume.UID),
		StorageClassName: class.Name, StorageClassUID: string(class.UID), Provisioner: class.Provisioner, CSIDriver: driver.Name, CSIDriverUID: string(driver.UID),
		CSIDriverSpecSHA256: resourceCanonicalDigest(driver.Spec), VolumeSourceSHA256: resourceCanonicalDigest(volume.Spec.PersistentVolumeSource),
		MountOptionsSHA256: resourceCanonicalDigest(volume.Spec.MountOptions), VolumeMode: "Filesystem"}
	return profile, claim, volume, class, driver
}

// TestResourceQualifiedPVCUsesFreshSelectedIdentityTLS exercises the real policy consumer,
// including fresh denied reads and a substituted PV after an earlier successful qualification.
// TestResourceQualifiedPVCUsesFreshSelectedIdentityTLS проверяет real policy consumer с fresh
// denied reads и подменой PV после предыдущей successful qualification.
func TestResourceQualifiedPVCUsesFreshSelectedIdentityTLS(t *testing.T) {
	profile, claim, volume, class, driver := storageEvidenceFixture()
	var changed, denied atomic.Bool
	var reads atomic.Int64
	credential, _ := transportFixtureWithHandler(t, false, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if resourcePolicyReviewFixture(t, writer, request, denied.Load()) {
			return
		}
		if request.Method != http.MethodGet {
			t.Error("qualification performed a mutation")
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		reads.Add(1)
		switch request.URL.Path {
		case "/api/v1/namespaces/example/persistentvolumeclaims/app":
			_ = json.NewEncoder(writer).Encode(claim)
		case "/api/v1/persistentvolumes/app-volume":
			current := volume.DeepCopy()
			if changed.Load() {
				current.Spec.ClaimRef.UID = "55555555-5555-4555-8555-555555555555"
			}
			_ = json.NewEncoder(writer).Encode(current)
		case "/apis/storage.k8s.io/v1/storageclasses/app-data":
			_ = json.NewEncoder(writer).Encode(class)
		case "/apis/storage.k8s.io/v1/csidrivers/csi.example":
			_ = json.NewEncoder(writer).Encode(driver)
		default:
			t.Error("qualification added an unregistered evidence read")
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	credential.target.ResourcePolicy = &managedkubernetes.ResourcePolicy{Storage: []managedkubernetes.StorageQualification{profile}}
	credential.target.ResourcePolicySHA256 = resourceCanonicalDigest(*credential.target.ResourcePolicy)
	address, object, spec := resourcePolicyFixture(t)
	spec["volumes"] = []any{map[string]any{"name": "app", "persistentVolumeClaim": map[string]any{"claimName": "app"}}}
	spec["containers"].([]any)[0].(map[string]any)["volumeMounts"] = []any{map[string]any{"name": "app", "mountPath": "/data"}}
	if ValidateResourceObjectPolicy("kubernetes.patch", address, "Deployment", object, nil) == nil {
		t.Fatal("unqualified static PVC acquired runnable admission")
	}
	check := func() error {
		return credential.ValidateQualifiedResourcePolicy(context.Background(), "kubernetes.patch", address, "Deployment", strings.Repeat("a", 64), subjectPin(t), object, nil)
	}
	if check() != nil || reads.Load() != 4 {
		t.Fatal("qualified ordinary application PVC did not use four exact selected-identity reads")
	}
	changed.Store(true)
	if check() == nil || reads.Load() != 8 {
		t.Fatal("substituted fresh PV reused prior successful qualification")
	}
	denied.Store(true)
	if check() == nil || reads.Load() != 8 {
		t.Fatal("denied selected identity fell back to an operator credential or cached proof")
	}
}

// TestResourceQualifiedControllerUsesWholeTLSInventory proves the actual custom-resource policy
// consumer joins full selected-identity inventories and refuses a later desired-only snapshot.
// TestResourceQualifiedControllerUsesWholeTLSInventory доказывает actual custom-resource policy
// consumer через full selected-identity inventory и отказ от позднего desired-only snapshot.
func TestResourceQualifiedControllerUsesWholeTLSInventory(t *testing.T) {
	profile, deployment, replicas, pods := controllerEvidenceFixture()
	var missing atomic.Bool
	var reads atomic.Int64
	credential, _ := transportFixtureWithHandler(t, false, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if resourcePolicyReviewFixture(t, writer, request, false) {
			return
		}
		if request.Method != http.MethodGet || request.URL.RawQuery != "" {
			t.Error("controller evidence used a mutation or selector-filtered inventory")
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		reads.Add(1)
		switch request.URL.Path {
		case "/api/v1/namespaces/operator/pods":
			actual := pods
			if missing.Load() {
				actual = nil
			}
			_ = json.NewEncoder(writer).Encode(corev1.PodList{TypeMeta: metav1.TypeMeta{Kind: "PodList", APIVersion: "v1"}, Items: actual})
		case "/apis/apps/v1/namespaces/operator/deployments/controller":
			_ = json.NewEncoder(writer).Encode(deployment)
		case "/apis/apps/v1/namespaces/operator/replicasets":
			_ = json.NewEncoder(writer).Encode(appsv1.ReplicaSetList{TypeMeta: metav1.TypeMeta{Kind: "ReplicaSetList", APIVersion: "apps/v1"}, Items: replicas})
		default:
			t.Error("controller qualification reached an unregistered evidence route")
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	credential.target.ResourcePolicy = &managedkubernetes.ResourcePolicy{Controllers: []managedkubernetes.ControllerQualification{profile}}
	credential.target.ResourcePolicySHA256 = resourceCanonicalDigest(*credential.target.ResourcePolicy)
	address := ResourceAddress{Group: profile.Group, Version: profile.Version, Resource: profile.Resource, Namespace: "example", Namespaced: true, Name: "selected"}
	object := map[string]any{"apiVersion": "apps.example/v1", "kind": "Widget", "metadata": map[string]any{"name": "selected", "namespace": "example"}, "spec": map[string]any{"desired": "safe"}}
	check := func() error {
		return credential.ValidateQualifiedResourcePolicy(context.Background(), "kubernetes.patch", address, "Widget", profile.SchemaSHA256, subjectPin(t), object, []string{"operator"})
	}
	if check() != nil || reads.Load() != 3 {
		t.Fatal("exact qualified controller was not joined through real TLS")
	}
	missing.Store(true)
	if err := check(); err == nil || ResourcePolicyReason(err) != managedkubernetes.ReasonControllerRuntimeUnavailable || reads.Load() != 6 {
		t.Fatal("desired-only controller reused previous serving evidence")
	}
	missing.Store(false)
	profile.Consumer, profile.Extractor = managedkubernetes.ControllerConsumerPodTemplateV1, "SPEC_TEMPLATE"
	credential.target.ResourcePolicy.Controllers[0] = profile
	credential.target.ResourcePolicySHA256 = resourceCanonicalDigest(*credential.target.ResourcePolicy)
	_, _, podSpec := resourcePolicyFixture(t)
	template := map[string]any{"metadata": map[string]any{"namespace": "example"}, "spec": podSpec}
	object["spec"] = map[string]any{"template": template}
	if check() != nil {
		t.Fatal("qualified same-namespace Pod-template consumer was rejected")
	}
	template["metadata"].(map[string]any)["namespace"] = "private"
	if check() == nil {
		t.Fatal("custom controller manufactured a foreign Pod namespace from one grant")
	}
	template["metadata"].(map[string]any)["namespace"] = ""
	address.Namespace, address.Namespaced = "", false
	object["metadata"].(map[string]any)["namespace"] = ""
	if check() == nil {
		t.Fatal("cluster CRD manufactured implicit namespace fan-out authority")
	}
}

// TestResourceStatefulControllerJoinsStableRevision verifies the second finite controller kind
// without accepting a mismatched rollout revision or an invented PVC-template qualification.
// TestResourceStatefulControllerJoinsStableRevision проверяет второй finite controller kind
// без mismatched rollout revision либо выдуманной PVC-template qualification.
func TestResourceStatefulControllerJoinsStableRevision(t *testing.T) {
	profile, deployment, _, pods := controllerEvidenceFixture()
	profile.ControllerKind = "StatefulSet"
	count, controlled := int32(1), true
	stateful := appsv1.StatefulSet{TypeMeta: metav1.TypeMeta{Kind: "StatefulSet", APIVersion: "apps/v1"}, ObjectMeta: deployment.ObjectMeta, Spec: appsv1.StatefulSetSpec{Replicas: &count, ServiceName: "controller-service", Template: deployment.Spec.Template}, Status: appsv1.StatefulSetStatus{ObservedGeneration: 1, Replicas: 1, ReadyReplicas: 1, CurrentReplicas: 1, UpdatedReplicas: 1, CurrentRevision: "revision1", UpdateRevision: "revision1"}}
	pods[0].OwnerReferences = []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "StatefulSet", UID: deployment.UID, Name: deployment.Name, Controller: &controlled}}
	pods[0].Spec.Hostname, pods[0].Spec.Subdomain = pods[0].Name, "controller-service"
	if validateResourceControllerServing(profile, nil, &stateful, nil, pods) != nil {
		t.Fatal("exact stable StatefulSet evidence was rejected")
	}
	stateful.Status.UpdateRevision = "revision2"
	if validateResourceControllerServing(profile, nil, &stateful, nil, pods) == nil {
		t.Fatal("unfinished StatefulSet rollout was qualified")
	}
}

// resourcePolicyReviewFixture implements only synthetic selected-identity reviews; every
// caller still goes through production Authorize, exact TLS and request construction.
// resourcePolicyReviewFixture реализует лишь synthetic selected-identity reviews; каждый
// caller проходит production Authorize, exact TLS и request construction.
func resourcePolicyReviewFixture(t *testing.T, writer http.ResponseWriter, request *http.Request, denied bool) bool {
	t.Helper()
	switch request.URL.Path {
	case "/apis/authentication.k8s.io/v1/selfsubjectreviews":
		_ = json.NewEncoder(writer).Encode(authenticationv1.SelfSubjectReview{TypeMeta: metav1.TypeMeta{Kind: "SelfSubjectReview", APIVersion: "authentication.k8s.io/v1"}, Status: authenticationv1.SelfSubjectReviewStatus{UserInfo: subjectFixture()}})
		return true
	case "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews":
		var review authorizationv1.SelfSubjectAccessReview
		if json.NewDecoder(request.Body).Decode(&review) != nil {
			t.Error("invalid synthetic qualification review")
		}
		review.Status.Allowed, review.Status.Denied = !denied, denied
		_ = json.NewEncoder(writer).Encode(review)
		return true
	}
	return false
}

// TestResourceStorageEvidenceRequiresExactFreshChain covers ordinary PVC admission and every
// material substitution boundary, including matching annotations that cannot prove provisioner.
// TestResourceStorageEvidenceRequiresExactFreshChain проверяет ordinary PVC admission и каждую
// material substitution boundary, включая annotations, не доказывающие provisioner.
func TestResourceStorageEvidenceRequiresExactFreshChain(t *testing.T) {
	profile, claim, volume, class, driver := storageEvidenceFixture()
	if validateResourceStorageEvidence(profile, claim, volume, class, driver) != nil {
		t.Fatal("qualified application-data CSI chain was rejected")
	}
	block := corev1.PersistentVolumeBlock
	claim.Spec.VolumeMode, volume.Spec.VolumeMode = &block, &block
	profile.VolumeMode = "Block"
	if validateResourceStorageEvidence(profile, claim, volume, class, driver) != nil {
		t.Fatal("qualified application-data Block CSI chain was rejected")
	}
	for _, failure := range []string{"pvc UID", "pv UID", "claim UID", "claim namespace", "class UID", "provisioner", "driver UID", "driver spec", "driver token", "source drift", "mixed hostPath", "CSI secret", "mount drift", "mode", "readOnly", "unbound"} {
		t.Run(failure, func(t *testing.T) {
			profile, claim, volume, class, driver := storageEvidenceFixture()
			switch failure {
			case "pvc UID":
				claim.UID = "55555555-5555-4555-8555-555555555555"
			case "pv UID":
				volume.UID = "55555555-5555-4555-8555-555555555555"
			case "claim UID":
				volume.Spec.ClaimRef.UID = "55555555-5555-4555-8555-555555555555"
			case "claim namespace":
				volume.Spec.ClaimRef.Namespace = "private"
			case "class UID":
				class.UID = "55555555-5555-4555-8555-555555555555"
			case "provisioner":
				class.Provisioner = "foreign.example"
				class.Annotations = map[string]string{"volume.kubernetes.io/storage-provisioner": profile.Provisioner}
			case "driver UID":
				driver.UID = "55555555-5555-4555-8555-555555555555"
			case "driver spec":
				attached := true
				driver.Spec.AttachRequired = &attached
			case "driver token":
				driver.Spec.TokenRequests = []storagev1.TokenRequest{{Audience: "private"}}
				profile.CSIDriverSpecSHA256 = resourceCanonicalDigest(driver.Spec)
			case "source drift":
				volume.Spec.CSI.VolumeAttributes = map[string]string{"foreign": "value"}
			case "mixed hostPath":
				volume.Spec.HostPath = &corev1.HostPathVolumeSource{Path: "/private"}
				profile.VolumeSourceSHA256 = resourceCanonicalDigest(volume.Spec.PersistentVolumeSource)
			case "CSI secret":
				volume.Spec.CSI.NodePublishSecretRef = &corev1.SecretReference{Name: "private", Namespace: "private"}
				profile.VolumeSourceSHA256 = resourceCanonicalDigest(volume.Spec.PersistentVolumeSource)
			case "mount drift":
				volume.Spec.MountOptions = []string{"unsafe"}
			case "mode":
				mode := corev1.PersistentVolumeBlock
				volume.Spec.VolumeMode = &mode
			case "readOnly":
				volume.Spec.CSI.ReadOnly = true
			case "unbound":
				claim.Status.Phase = corev1.ClaimPending
			}
			if validateResourceStorageEvidence(profile, claim, volume, class, driver) == nil {
				t.Fatal("substituted or credential-bearing CSI chain was accepted")
			}
		})
	}
}

// TestResourceStorageModeCannotConfuseMountAndDevice accepts only the broker-verified mode,
// including Block devices, without allowing caller volume names to manufacture qualification.
// TestResourceStorageModeCannotConfuseMountAndDevice принимает только broker-verified mode,
// включая Block devices; caller volume names не создают qualification.
func TestResourceStorageModeCannotConfuseMountAndDevice(t *testing.T) {
	address, object, spec := resourcePolicyFixture(t)
	spec["volumes"] = []any{map[string]any{"name": "app", "persistentVolumeClaim": map[string]any{"claimName": "app"}}}
	container := spec["containers"].([]any)[0].(map[string]any)
	container["volumeDevices"] = []any{map[string]any{"name": "app", "devicePath": "/dev/app"}}
	if validateResourceObjectPolicy("kubernetes.patch", address, "Deployment", object, nil, map[string]bool{"app": true}, nil, false) != nil {
		t.Fatal("verified Block device was rejected")
	}
	if validateResourceObjectPolicy("kubernetes.patch", address, "Deployment", object, nil, map[string]bool{"app": false}, nil, false) == nil {
		t.Fatal("Filesystem qualification authorized a raw block device")
	}
	delete(container, "volumeDevices")
	container["volumeMounts"] = []any{map[string]any{"name": "app", "mountPath": "/data"}}
	if validateResourceObjectPolicy("kubernetes.patch", address, "Deployment", object, nil, map[string]bool{"app": false}, nil, false) != nil {
		t.Fatal("verified Filesystem mount was rejected")
	}
	if validateResourceObjectPolicy("kubernetes.patch", address, "Deployment", object, nil, map[string]bool{"app": true}, nil, false) == nil {
		t.Fatal("Block qualification authorized a filesystem mount")
	}
}

// controllerEvidenceFixture provides one complete stable Deployment/ReplicaSet/Pod owner join.
// controllerEvidenceFixture даёт один complete stable Deployment/ReplicaSet/Pod owner join.
func controllerEvidenceFixture() (managedkubernetes.ControllerQualification, appsv1.Deployment, []appsv1.ReplicaSet, []corev1.Pod) {
	count, controlling, noToken := int32(1), true, false
	image := strings.Repeat("a", 64)
	template := corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "controller"}},
		Spec: corev1.PodSpec{ServiceAccountName: "controller", AutomountServiceAccountToken: &noToken, Containers: []corev1.Container{{Name: "controller", Image: "controller@sha256:" + image}}}}
	deployment := appsv1.Deployment{TypeMeta: metav1.TypeMeta{Kind: "Deployment", APIVersion: "apps/v1"},
		ObjectMeta: metav1.ObjectMeta{Name: "controller", Namespace: "operator", UID: "11111111-1111-4111-8111-111111111111", ResourceVersion: "10", Generation: 1},
		Spec:       appsv1.DeploymentSpec{Replicas: &count, Template: template}, Status: appsv1.DeploymentStatus{ObservedGeneration: 1, Replicas: 1, ReadyReplicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1}}
	replica := appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "controller-rs", Namespace: "operator", UID: "22222222-2222-4222-8222-222222222222", ResourceVersion: "20", Generation: 1,
		OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: deployment.Name, UID: deployment.UID, Controller: &controlling}}},
		Spec: appsv1.ReplicaSetSpec{Replicas: &count, Template: *template.DeepCopy()}, Status: appsv1.ReplicaSetStatus{ObservedGeneration: 1, Replicas: 1, ReadyReplicas: 1, AvailableReplicas: 1}}
	replica.Spec.Template.Labels[appsv1.DefaultDeploymentUniqueLabelKey] = "synthetic"
	pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "controller-pod", Namespace: "operator", UID: "33333333-3333-4333-8333-333333333333", ResourceVersion: "30",
		OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: replica.Name, UID: replica.UID, Controller: &controlling}}}, Spec: *template.Spec.DeepCopy(),
		Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			ContainerStatuses: []corev1.ContainerStatus{{Name: "controller", Image: template.Spec.Containers[0].Image, ImageID: "registry/controller@sha256:" + image, ContainerID: "containerd://synthetic", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}}}
	pod.Spec.NodeName = "node"
	profile := managedkubernetes.ControllerQualification{Group: "apps.example", Version: "v1", Resource: "widgets", Kind: "Widget", SchemaSHA256: strings.Repeat("b", 64),
		ControllerKind: "Deployment", ControllerNamespace: deployment.Namespace, ControllerName: deployment.Name, ControllerUID: string(deployment.UID), ControllerContainer: "controller",
		ControllerImageDigest: image, TemplateSHA256: resourceCanonicalDigest(template), Consumer: managedkubernetes.ControllerConsumerDataOnlyV1, Extractor: "NONE"}
	return profile, deployment, []appsv1.ReplicaSet{replica}, []corev1.Pod{pod}
}

// TestResourceControllerRequiresPositiveServingJoin denies desired-only counters and every
// foreign/old/debug/unready/runtime-image drift while retaining an exact stable controller.
// TestResourceControllerRequiresPositiveServingJoin отклоняет desired-only counters и каждый
// foreign/old/debug/unready/runtime-image drift, сохраняя exact stable controller.
func TestResourceControllerRequiresPositiveServingJoin(t *testing.T) {
	profile, deployment, replicas, pods := controllerEvidenceFixture()
	if validateResourceControllerServing(profile, &deployment, nil, replicas, pods) != nil {
		t.Fatal("complete stable controller evidence was rejected")
	}
	for _, failure := range []string{"no pods", "no RS", "UID", "image", "unready", "extra container", "ephemeral", "template", "foreign SA", "old RS", "duplicate pod", "unknown phase"} {
		t.Run(failure, func(t *testing.T) {
			profile, deployment, replicas, pods := controllerEvidenceFixture()
			switch failure {
			case "no pods":
				pods = nil
			case "no RS":
				replicas = nil
			case "UID":
				deployment.UID = "44444444-4444-4444-8444-444444444444"
			case "image":
				pods[0].Status.ContainerStatuses[0].ImageID = "registry/controller@sha256:" + strings.Repeat("c", 64)
			case "unready":
				pods[0].Status.ContainerStatuses[0].Ready = false
			case "extra container":
				pods[0].Spec.Containers = append(pods[0].Spec.Containers, corev1.Container{Name: "debug", Image: "debug"})
			case "ephemeral":
				pods[0].Spec.EphemeralContainers = []corev1.EphemeralContainer{{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "debug"}}}
			case "template":
				deployment.Spec.Template.Spec.HostNetwork = true
			case "foreign SA":
				foreign := *pods[0].DeepCopy()
				foreign.OwnerReferences, foreign.Name = nil, "foreign"
				pods = append(pods, foreign)
			case "old RS":
				zero := int32(0)
				replicas[0].Spec.Replicas = &zero
				replicas[0].Status.Replicas = 0
			case "duplicate pod":
				pods = append(pods, *pods[0].DeepCopy())
			case "unknown phase":
				pods[0].Status.Phase = ""
			}
			if validateResourceControllerServing(profile, &deployment, nil, replicas, pods) == nil {
				t.Fatal("incomplete or substituted serving evidence was accepted")
			}
		})
	}
}
