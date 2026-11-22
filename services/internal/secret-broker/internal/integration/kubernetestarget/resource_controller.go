package kubernetestarget

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// resourceControllerRuntimeUnavailable separates missing positive serving evidence from a
// schema-valid custom resource. A desired Deployment alone never supplies this proof.
// resourceControllerRuntimeUnavailable отделяет missing positive serving evidence от
// schema-valid custom resource. Сам desired Deployment не даёт такое proof.
const resourceControllerRuntimeUnavailable = "managed Kubernetes controller runtime is unavailable"

// resourceControllerQualification resolves one exact private operator classification and joins
// its current controller, whole namespace ReplicaSets and Pods using Kubernetes owner UIDs.
// Label selectors and ready counters never replace the full positive serving inventory.
// resourceControllerQualification разрешает exact private operator classification и соединяет
// current controller, все namespace ReplicaSets/Pods по Kubernetes owner UID. Label selectors
// и ready counters не заменяют full positive serving inventory.
func (credential Credential) resourceControllerQualification(ctx context.Context, address ResourceAddress, kind, schemaDigest, subject string) (managedkubernetes.ControllerQualification, error) {
	var expected *managedkubernetes.ControllerQualification
	if credential.target.ResourcePolicy != nil {
		for index := range credential.target.ResourcePolicy.Controllers {
			candidate := &credential.target.ResourcePolicy.Controllers[index]
			if candidate.Group == address.Group && candidate.Version == address.Version && candidate.Resource == address.Resource && candidate.Kind == kind && candidate.SchemaSHA256 == schemaDigest {
				expected = candidate
			}
		}
	}
	if expected == nil {
		return managedkubernetes.ControllerQualification{}, errors.New(resourceControllerPolicyRequired)
	}
	var pods corev1.PodList
	if credential.readResourcePolicyCollection(ctx, subject, ResourceAddress{Version: "v1", Resource: "pods", Namespaced: true, Namespace: expected.ControllerNamespace}, &pods) != nil ||
		pods.Kind != "PodList" || pods.APIVersion != "v1" || pods.Continue != "" || len(pods.Items) > 1000 {
		return managedkubernetes.ControllerQualification{}, errors.New(resourceControllerRuntimeUnavailable)
	}
	controller := ResourceAddress{Group: "apps", Version: "v1", Namespaced: true, Namespace: expected.ControllerNamespace, Name: expected.ControllerName}
	if expected.ControllerKind == "Deployment" {
		controller.Resource = "deployments"
		var deployment appsv1.Deployment
		var replicas appsv1.ReplicaSetList
		if credential.readResourcePolicyObject(ctx, subject, controller, &deployment) != nil ||
			credential.readResourcePolicyCollection(ctx, subject, ResourceAddress{Group: "apps", Version: "v1", Resource: "replicasets", Namespaced: true, Namespace: expected.ControllerNamespace}, &replicas) != nil ||
			replicas.Kind != "ReplicaSetList" || replicas.APIVersion != "apps/v1" || replicas.Continue != "" || len(replicas.Items) > 1000 ||
			validateResourceControllerServing(*expected, &deployment, nil, replicas.Items, pods.Items) != nil {
			return managedkubernetes.ControllerQualification{}, errors.New(resourceControllerRuntimeUnavailable)
		}
	} else {
		controller.Resource = "statefulsets"
		var stateful appsv1.StatefulSet
		if credential.readResourcePolicyObject(ctx, subject, controller, &stateful) != nil || validateResourceControllerServing(*expected, nil, &stateful, nil, pods.Items) != nil {
			return managedkubernetes.ControllerQualification{}, errors.New(resourceControllerRuntimeUnavailable)
		}
	}
	return *expected, nil
}

// readResourcePolicyCollection reads the full bounded namespace inventory without selectors.
// A continued page is refused by the caller rather than silently ignoring unknown tail Pods.
// readResourcePolicyCollection читает full bounded namespace inventory без selectors.
// Continued page отклоняет caller, не игнорируя unknown tail Pods молча.
func (credential Credential) readResourcePolicyCollection(ctx context.Context, subject string, address ResourceAddress, output any) error {
	requirements, err := OperationAccessRequirements("kubernetes.list", address)
	if err != nil || credential.Authorize(ctx, subject, requirements) != nil {
		return errors.New(resourceControllerRuntimeUnavailable)
	}
	// readResourcePolicyObject owns strict TLS/JSON decoding; its explicit GET review on a
	// collection is not used because Kubernetes authorizes collection access as list.
	// readResourcePolicyObject владеет strict TLS/JSON decode; его GET review не применяется
	// к collection, поскольку Kubernetes авторизует collection access как list.
	client, closeIdle, err := credential.NewClient(30 * time.Second)
	if err != nil {
		return errors.New(resourceControllerRuntimeUnavailable)
	}
	defer closeIdle()
	request, err := credential.ReadRequest(ctx, address)
	if err != nil {
		return errors.New(resourceControllerRuntimeUnavailable)
	}
	return decodeResourcePolicyHTTP(client, request, output)
}

// validateResourceControllerServing requires exact immutable controller/template metadata and
// a joined stable Pod set, including real container image IDs. Extra old/debug/unready Pods or
// a foreign producer using the controller service account close the qualification.
// validateResourceControllerServing требует exact immutable controller/template metadata и
// joined stable Pod set с real container image IDs. Extra old/debug/unready Pods либо foreign
// producer с controller service account закрывают qualification.
func validateResourceControllerServing(expected managedkubernetes.ControllerQualification, deployment *appsv1.Deployment, stateful *appsv1.StatefulSet, replicas []appsv1.ReplicaSet, pods []corev1.Pod) error {
	if expected.Validate() != nil || (deployment == nil) == (stateful == nil) {
		return errors.New(resourceControllerRuntimeUnavailable)
	}
	var metadata metav1.ObjectMeta
	var template corev1.PodTemplateSpec
	var desired int32
	owners := map[string]bool{}
	serviceName := ""
	if deployment != nil {
		metadata, template = deployment.ObjectMeta, deployment.Spec.Template
		if expected.ControllerKind != "Deployment" || deployment.Kind != "Deployment" || deployment.APIVersion != "apps/v1" || deployment.Spec.Replicas == nil || deployment.Status.ObservedGeneration != deployment.Generation {
			return errors.New(resourceControllerRuntimeUnavailable)
		}
		desired = *deployment.Spec.Replicas
		if deployment.Status.Replicas != desired || deployment.Status.UpdatedReplicas != desired || deployment.Status.ReadyReplicas != desired || deployment.Status.AvailableReplicas != desired || deployment.Status.UnavailableReplicas != 0 {
			return errors.New(resourceControllerRuntimeUnavailable)
		}
		for _, replica := range replicas {
			if !resourceOwnedBy(replica.OwnerReferences, expected.ControllerUID, "Deployment") {
				continue
			}
			if replica.Namespace != expected.ControllerNamespace || replica.UID == "" || replica.Spec.Replicas == nil {
				return errors.New(resourceControllerRuntimeUnavailable)
			}
			if *replica.Spec.Replicas == 0 && replica.Status.Replicas == 0 {
				owners[string(replica.UID)] = false
				continue
			}
			candidate := replica.Spec.Template.DeepCopy()
			delete(candidate.Labels, appsv1.DefaultDeploymentUniqueLabelKey)
			if replica.DeletionTimestamp != nil || *replica.Spec.Replicas != desired || replica.Status.ObservedGeneration != replica.Generation || replica.Status.Replicas != desired || replica.Status.ReadyReplicas != desired || replica.Status.AvailableReplicas != desired || !reflect.DeepEqual(*candidate, template) {
				return errors.New(resourceControllerRuntimeUnavailable)
			}
			owners[string(replica.UID)] = true
		}
		active := 0
		for _, current := range owners {
			if current {
				active++
			}
		}
		if active != 1 {
			return errors.New(resourceControllerRuntimeUnavailable)
		}
	} else {
		metadata, template, serviceName = stateful.ObjectMeta, stateful.Spec.Template, stateful.Spec.ServiceName
		if expected.ControllerKind != "StatefulSet" || stateful.Kind != "StatefulSet" || stateful.APIVersion != "apps/v1" || stateful.Spec.Replicas == nil || stateful.Status.ObservedGeneration != stateful.Generation || len(stateful.Spec.VolumeClaimTemplates) != 0 {
			return errors.New(resourceControllerRuntimeUnavailable)
		}
		desired = *stateful.Spec.Replicas
		if stateful.Status.Replicas != desired || stateful.Status.ReadyReplicas != desired || stateful.Status.CurrentReplicas != desired || stateful.Status.UpdatedReplicas != desired || stateful.Status.CurrentRevision == "" || stateful.Status.CurrentRevision != stateful.Status.UpdateRevision {
			return errors.New(resourceControllerRuntimeUnavailable)
		}
		owners[expected.ControllerUID] = true
	}
	if metadata.Name != expected.ControllerName || metadata.Namespace != expected.ControllerNamespace || string(metadata.UID) != expected.ControllerUID || metadata.ResourceVersion == "" || metadata.Generation < 1 || metadata.DeletionTimestamp != nil || desired < 1 || desired > 32 || resourceCanonicalDigest(template) != expected.TemplateSHA256 {
		return errors.New(resourceControllerRuntimeUnavailable)
	}
	count := int32(0)
	seen := map[string]bool{}
	for _, pod := range pods {
		owned, active := false, false
		for uid, current := range owners {
			kind := "ReplicaSet"
			if stateful != nil {
				kind = "StatefulSet"
			}
			if resourceOwnedBy(pod.OwnerReferences, uid, kind) {
				owned, active = true, current
			}
		}
		if !owned {
			if pod.Namespace == expected.ControllerNamespace && pod.Spec.ServiceAccountName == template.Spec.ServiceAccountName {
				return errors.New(resourceControllerRuntimeUnavailable)
			}
			continue
		}
		if !active || seen[string(pod.UID)] || !resourceControllerPodReady(pod, template.Spec, expected, serviceName) {
			return errors.New(resourceControllerRuntimeUnavailable)
		}
		seen[string(pod.UID)] = true
		count++
	}
	if count != desired {
		return errors.New(resourceControllerRuntimeUnavailable)
	}
	return nil
}

// resourceOwnedBy accepts one exact controller edge and rejects ambiguous controlling owners.
// resourceOwnedBy принимает одно exact controller edge, отклоняя ambiguous controlling owners.
func resourceOwnedBy(owners []metav1.OwnerReference, uid, kind string) bool {
	matched := false
	for _, owner := range owners {
		if owner.Controller != nil && *owner.Controller {
			if matched || string(owner.UID) != uid || owner.Kind != kind || owner.APIVersion != "apps/v1" {
				return false
			}
			matched = true
		}
	}
	return matched
}

// resourceControllerPodReady compares full admitted Pod spec after the finite controller-added
// scheduling/name fields, then checks real ready/running/terminated-init states and image IDs.
// Admission-added volumes/sidecars are not silently ignored; they require a new qualified template.
// resourceControllerPodReady сверяет full admitted Pod spec за вычетом finite controller-added
// scheduling/name fields, затем real ready/running/init-terminal states и image IDs. Добавленные
// admission volumes/sidecars не игнорируются: нужен новый qualified template.
func resourceControllerPodReady(pod corev1.Pod, template corev1.PodSpec, expected managedkubernetes.ControllerQualification, serviceName string) bool {
	if pod.Namespace != expected.ControllerNamespace || pod.UID == "" || pod.ResourceVersion == "" || pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning || pod.Spec.NodeName == "" || len(pod.Spec.EphemeralContainers) != 0 || len(pod.Status.EphemeralContainerStatuses) != 0 {
		return false
	}
	actual := *pod.Spec.DeepCopy()
	actual.NodeName = template.NodeName
	if serviceName != "" {
		if actual.Hostname != pod.Name || actual.Subdomain != serviceName {
			return false
		}
		actual.Hostname, actual.Subdomain = template.Hostname, template.Subdomain
	}
	if !reflect.DeepEqual(actual, template) || len(pod.Status.ContainerStatuses) != len(template.Containers) || len(pod.Status.InitContainerStatuses) != len(template.InitContainers) {
		return false
	}
	ready := false
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			if ready || condition.Status != corev1.ConditionTrue {
				return false
			}
			ready = true
		}
	}
	if !ready {
		return false
	}
	mainFound := false
	for _, group := range []struct {
		containers []corev1.Container
		statuses   []corev1.ContainerStatus
		init       bool
	}{{template.Containers, pod.Status.ContainerStatuses, false}, {template.InitContainers, pod.Status.InitContainerStatuses, true}} {
		for _, container := range group.containers {
			imageDigest := ""
			if container.Name == expected.ControllerContainer {
				mainFound, imageDigest = true, expected.ControllerImageDigest
			} else if _, suffix, found := strings.Cut(container.Image, "@sha256:"); found && managedkubernetes.ValidDigest(suffix) {
				imageDigest = suffix
			}
			if imageDigest == "" {
				return false
			}
			matches := 0
			for _, status := range group.statuses {
				if status.Name != container.Name {
					continue
				}
				matches++
				if !strings.HasSuffix(status.ImageID, "@sha256:"+imageDigest) || status.ContainerID == "" ||
					group.init && (status.State.Terminated == nil || status.State.Terminated.ExitCode != 0 || status.State.Running != nil || status.State.Waiting != nil) ||
					!group.init && (!status.Ready || status.State.Running == nil || status.State.Terminated != nil || status.State.Waiting != nil || status.Started != nil && !*status.Started) {
					return false
				}
			}
			if matches != 1 {
				return false
			}
		}
	}
	return mainFound
}
