// Package integrationegress публикует owner-проекцию точных OpenAPI origins.
package integrationegress

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	shared "github.com/codex-k8s/kodex/libs/go/integrationegresspolicy"
	"github.com/codex-k8s/kodex/libs/go/mailpolicy"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
)

const (
	namespace            = "kodex-system"
	deploymentName       = "egress-gateway"
	openAPIServiceName   = "egress-gateway-openapi"
	generationLabel      = "kodex.dev/integration-egress-generation"
	generationAnnotation = "kodex.dev/integration-egress-generation"
	sourceAnnotation     = "kodex.dev/integration-egress-source-digest"
)

var (
	ErrInvalid     = errors.New("integration egress projection is invalid")
	ErrConflict    = errors.New("integration egress projection conflicts with current generation")
	ErrUnavailable = errors.New("integration egress projection is unavailable")
)

type Kubernetes struct{ client kubernetes.Interface }

func InCluster(timeout time.Duration) (*Kubernetes, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, ErrUnavailable
	}
	config.Timeout = timeout
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, ErrUnavailable
	}
	return New(client)
}

func New(client kubernetes.Interface) (*Kubernetes, error) {
	if client == nil {
		return nil, ErrInvalid
	}
	return &Kubernetes{client: client}, nil
}

func sameJSON(left, right any) bool {
	a, err := json.Marshal(left)
	if err != nil {
		return false
	}
	b, err := json.Marshal(right)
	return err == nil && bytes.Equal(a, b)
}

func (publisher *Kubernetes) CheckAdmission(ctx context.Context) error {
	registered := []struct {
		name   string
		source func() (map[string]any, map[string]any)
	}{
		{mailpolicy.PublicationAdmissionName, mailpolicy.PublicationAdmissionResources},
		{shared.PublicationAdmissionName, shared.PublicationAdmissionResources},
		{shared.CreationBoundaryName, shared.CreationBoundaryResources},
	}
	for _, item := range registered {
		policy, err := publisher.client.AdmissionregistrationV1().ValidatingAdmissionPolicies().Get(ctx, item.name, metav1.GetOptions{})
		if err != nil {
			return ErrUnavailable
		}
		binding, err := publisher.client.AdmissionregistrationV1().ValidatingAdmissionPolicyBindings().Get(ctx, item.name, metav1.GetOptions{})
		if err != nil {
			return ErrUnavailable
		}
		policySource, bindingSource := item.source()
		policyRaw, _ := json.Marshal(policySource)
		bindingRaw, _ := json.Marshal(bindingSource)
		var expectedPolicy admissionv1.ValidatingAdmissionPolicy
		var expectedBinding admissionv1.ValidatingAdmissionPolicyBinding
		if json.Unmarshal(policyRaw, &expectedPolicy) != nil || json.Unmarshal(bindingRaw, &expectedBinding) != nil {
			return ErrInvalid
		}
		if policy.UID == "" || policy.ResourceVersion == "" || policy.Generation < 1 ||
			policy.Status.ObservedGeneration != policy.Generation || policy.Status.TypeChecking == nil ||
			len(policy.Status.TypeChecking.ExpressionWarnings) != 0 || binding.UID == "" || binding.ResourceVersion == "" ||
			!sameJSON(policy.Spec, expectedPolicy.Spec) || !sameJSON(binding.Spec, expectedBinding.Spec) {
			return ErrConflict
		}
	}
	return nil
}

func projectionObjects(document shared.Document) (corev1.ConfigMap, networkingv1.NetworkPolicy, error) {
	var configMap corev1.ConfigMap
	var network networkingv1.NetworkPolicy
	files, err := shared.RenderFiles(document)
	if err != nil || json.Unmarshal(files["integration-configmap.json"], &configMap) != nil ||
		json.Unmarshal(files["integration-networkpolicy.json"], &network) != nil {
		return configMap, network, ErrInvalid
	}
	network.Annotations = map[string]string{generationAnnotation: strconv.FormatInt(document.Generation, 10), sourceAnnotation: document.SourceDigest}
	return configMap, network, nil
}

func checkFence(annotations map[string]string, document shared.Document) error {
	raw := annotations[generationAnnotation]
	if raw == "" {
		if document.Generation != 1 {
			return ErrConflict
		}
		return nil
	}
	generation, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || generation > document.Generation ||
		generation == document.Generation && annotations[sourceAnnotation] != document.SourceDigest {
		return ErrConflict
	}
	return nil
}

// Повторное применение исходного render может вернуть supplemental NP к
// точному пустому bootstrap-состоянию. Только этот шаблон допускает
// восстановление из более нового owner-документа; произвольная NP без fence
// закрыто отклоняется.
func checkNetworkFence(policy *networkingv1.NetworkPolicy, document shared.Document) error {
	if policy.Annotations[generationAnnotation] != "" {
		return checkFence(policy.Annotations, document)
	}
	selector := policy.Spec.PodSelector
	if len(policy.Spec.Egress) != 0 || len(policy.Spec.Ingress) != 0 ||
		len(policy.Spec.PolicyTypes) != 1 || policy.Spec.PolicyTypes[0] != networkingv1.PolicyTypeEgress ||
		len(selector.MatchExpressions) != 0 || len(selector.MatchLabels) != 2 ||
		selector.MatchLabels["app.kubernetes.io/name"] != deploymentName ||
		selector.MatchLabels["app.kubernetes.io/component"] != "platform-egress" {
		return ErrConflict
	}
	return nil
}

func checkGatewaySource(deployment *appsv1.Deployment, document shared.Document) error {
	if deployment.Name != deploymentName || deployment.Namespace != namespace ||
		deployment.Labels["app.kubernetes.io/name"] != deploymentName ||
		deployment.Labels["app.kubernetes.io/component"] != "platform-egress" {
		return ErrConflict
	}
	containers, basePins, files, mounts, listeners, ports := 0, 0, 0, 0, 0, 0
	for _, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name != deploymentName {
			continue
		}
		containers++
		for _, item := range container.Env {
			if item.Name == "EGRESS_GATEWAY_EXPECTED_POLICY_DIGEST" {
				basePins++
				if item.ValueFrom != nil || item.Value != document.GatewayPolicyDigest {
					return ErrConflict
				}
			}
			if item.Name == "EGRESS_GATEWAY_INTEGRATION_POLICY_FILE" {
				files++
				if item.ValueFrom != nil || item.Value != "/var/run/config/kodex/egress-gateway-integration/integration-policy.json" {
					return ErrConflict
				}
			}
			if item.Name == "EGRESS_GATEWAY_INTEGRATION_CONNECT_LISTEN" {
				listeners++
				if item.ValueFrom != nil || item.Value != ":8083" {
					return ErrConflict
				}
			}
		}
		for _, port := range container.Ports {
			if port.Name == "openapi-connect" {
				ports++
				if port.ContainerPort != 8083 || port.Protocol != corev1.ProtocolTCP {
					return ErrConflict
				}
			}
		}
		for _, mount := range container.VolumeMounts {
			if mount.Name == "integration-policy" {
				mounts++
				if mount.MountPath != "/var/run/config/kodex/egress-gateway-integration" || !mount.ReadOnly ||
					mount.SubPath != "" || mount.SubPathExpr != "" {
					return ErrConflict
				}
			}
		}
	}
	if containers != 1 || basePins != 1 || files != 1 || mounts != 1 || listeners != 1 || ports != 1 {
		return ErrConflict
	}
	return nil
}

// Отдельный Service направляет OpenAPI CONNECT только в Pod с поколением,
// которое уже опубликовано владельцем. Остальные listener не меняют маршрут.
func checkOpenAPIServiceSource(service *corev1.Service, document shared.Document) error {
	if service.Name != openAPIServiceName || service.Namespace != namespace ||
		service.Labels["app.kubernetes.io/name"] != deploymentName ||
		service.Labels["app.kubernetes.io/component"] != "platform-egress" ||
		service.Spec.Type != corev1.ServiceTypeClusterIP || len(service.Spec.ExternalIPs) != 0 ||
		service.Spec.LoadBalancerIP != "" || service.Spec.PublishNotReadyAddresses ||
		len(service.Spec.Selector) != 3 ||
		service.Spec.Selector["app.kubernetes.io/name"] != deploymentName ||
		service.Spec.Selector["app.kubernetes.io/component"] != "platform-egress" ||
		len(service.Spec.Ports) != 1 || service.Spec.Ports[0].Name != "openapi-connect" ||
		service.Spec.Ports[0].Port != 8083 || service.Spec.Ports[0].NodePort != 0 ||
		service.Spec.Ports[0].Protocol != corev1.ProtocolTCP ||
		service.Spec.Ports[0].TargetPort.StrVal != "openapi-connect" || service.Spec.Ports[0].TargetPort.IntVal != 0 {
		return ErrConflict
	}
	current := service.Spec.Selector[generationLabel]
	if service.Annotations[generationAnnotation] == "" {
		// Только точный repo-owned bootstrap Service может восстановиться после
		// повторного render; иной unfenced selector не получает полномочие.
		if current != "1" {
			return ErrConflict
		}
		return nil
	}
	if current != service.Annotations[generationAnnotation] {
		return ErrConflict
	}
	return checkFence(service.Annotations, document)
}

func setOpenAPIService(service *corev1.Service, document shared.Document) error {
	if document.Validate() != nil {
		return ErrInvalid
	}
	if err := checkOpenAPIServiceSource(service, document); err != nil {
		return err
	}
	if service.Annotations == nil {
		service.Annotations = map[string]string{}
	}
	service.Annotations[generationAnnotation] = strconv.FormatInt(document.Generation, 10)
	service.Annotations[sourceAnnotation] = document.SourceDigest
	service.Spec.Selector[generationLabel] = strconv.FormatInt(document.Generation, 10)
	return nil
}

func setDeployment(deployment *appsv1.Deployment, document shared.Document, configMapName string) error {
	if document.Validate() != nil {
		return ErrInvalid
	}
	if checkGatewaySource(deployment, document) != nil || checkFence(deployment.Spec.Template.Annotations, document) != nil {
		return ErrConflict
	}
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = map[string]string{}
	}
	deployment.Spec.Template.Annotations[generationAnnotation] = strconv.FormatInt(document.Generation, 10)
	deployment.Spec.Template.Annotations[sourceAnnotation] = document.SourceDigest
	if deployment.Spec.Template.Labels == nil {
		deployment.Spec.Template.Labels = map[string]string{}
	}
	deployment.Spec.Template.Labels[generationLabel] = strconv.FormatInt(document.Generation, 10)
	containers := 0
	for index := range deployment.Spec.Template.Spec.Containers {
		container := &deployment.Spec.Template.Spec.Containers[index]
		if container.Name != deploymentName {
			continue
		}
		containers++
		found := 0
		for index := range container.Env {
			if container.Env[index].Name == "EGRESS_GATEWAY_INTEGRATION_POLICY_DIGEST" {
				found++
				container.Env[index].Value = document.Digest()
				container.Env[index].ValueFrom = nil
			}
		}
		if found > 1 {
			return ErrConflict
		}
		if found == 0 {
			container.Env = append(container.Env, corev1.EnvVar{Name: "EGRESS_GATEWAY_INTEGRATION_POLICY_DIGEST", Value: document.Digest()})
		}
	}
	if containers != 1 {
		return ErrConflict
	}
	volumes := 0
	for index := range deployment.Spec.Template.Spec.Volumes {
		volume := &deployment.Spec.Template.Spec.Volumes[index]
		if volume.Name != "integration-policy" {
			continue
		}
		volumes++
		mode := int32(0444)
		volume.VolumeSource = corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: configMapName}, DefaultMode: &mode,
			Items: []corev1.KeyToPath{{Key: "integration-policy.json", Path: "integration-policy.json"}},
		}}
	}
	if volumes != 1 {
		return ErrConflict
	}
	return nil
}

func (publisher *Kubernetes) checkConfigMap(ctx context.Context, expected corev1.ConfigMap) error {
	current, err := publisher.client.CoreV1().ConfigMaps(namespace).Get(ctx, expected.Name, metav1.GetOptions{})
	if err != nil {
		return ErrUnavailable
	}
	if current.UID == "" || current.ResourceVersion == "" || current.Immutable == nil || !*current.Immutable ||
		len(current.BinaryData) != 0 || len(current.OwnerReferences) != 0 ||
		!sameJSON(current.Data, expected.Data) || !validProjectionLabels(current.Labels, expected.Labels) {
		return ErrConflict
	}
	return nil
}

// Repo-owned local render добавляет provenance labels к неизменяемому
// bootstrap ConfigMap. Разрешён только этот закрытый набор с точными
// значениями; controller-created ConfigMap остаётся без добавочных labels.
func validProjectionLabels(current, expected map[string]string) bool {
	if current["app.kubernetes.io/name"] != expected["app.kubernetes.io/name"] ||
		current["app.kubernetes.io/component"] != expected["app.kubernetes.io/component"] {
		return false
	}
	allowed := map[string]string{
		"app.kubernetes.io/part-of":  "kodex",
		"kodex.dev/environment":      "staging",
		"kodex.dev/local-profile":    "hot-reload",
		"kodex.dev/security-profile": "trusted-cluster",
	}
	for key, value := range current {
		if want, exists := expected[key]; exists {
			if value != want {
				return false
			}
			continue
		}
		if key == "kodex.dev/profile" {
			if value != "web-only" && value != "web-with-mattermost" {
				return false
			}
			continue
		}
		if allowed[key] != value || value == "" {
			return false
		}
	}
	return true
}

func (publisher *Kubernetes) Publish(ctx context.Context, document shared.Document) error {
	if document.Validate() != nil {
		return ErrInvalid
	}
	if err := publisher.CheckAdmission(ctx); err != nil {
		return err
	}
	configMap, network, err := projectionObjects(document)
	if err != nil {
		return err
	}
	current, err := publisher.client.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		return ErrUnavailable
	}
	if checkGatewaySource(current, document) != nil || checkFence(current.Spec.Template.Annotations, document) != nil {
		return ErrConflict
	}
	if _, err := publisher.client.CoreV1().ConfigMaps(namespace).Create(ctx, &configMap, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return ErrUnavailable
	}
	if err := publisher.checkConfigMap(ctx, configMap); err != nil {
		return err
	}
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, err := publisher.client.NetworkingV1().NetworkPolicies(namespace).Get(ctx, shared.NetworkPolicyName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if current.Labels["app.kubernetes.io/name"] != deploymentName ||
			current.Labels["app.kubernetes.io/component"] != "platform-egress" ||
			checkNetworkFence(current, document) != nil {
			return ErrConflict
		}
		if sameJSON(current.Spec, network.Spec) && current.Annotations[generationAnnotation] == network.Annotations[generationAnnotation] &&
			current.Annotations[sourceAnnotation] == network.Annotations[sourceAnnotation] {
			return nil
		}
		current.Spec = network.Spec
		if current.Annotations == nil {
			current.Annotations = map[string]string{}
		}
		current.Annotations[generationAnnotation] = network.Annotations[generationAnnotation]
		current.Annotations[sourceAnnotation] = network.Annotations[sourceAnnotation]
		_, err = publisher.client.NetworkingV1().NetworkPolicies(namespace).Update(ctx, current, metav1.UpdateOptions{})
		return err
	}); err != nil {
		return ErrConflict
	}
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, err := publisher.client.CoreV1().Services(namespace).Get(ctx, openAPIServiceName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		expected := current.DeepCopy()
		if err := setOpenAPIService(expected, document); err != nil {
			return err
		}
		if sameJSON(current.Spec, expected.Spec) && sameJSON(current.Annotations, expected.Annotations) {
			return nil
		}
		_, err = publisher.client.CoreV1().Services(namespace).Update(ctx, expected, metav1.UpdateOptions{})
		return err
	}); err != nil {
		return ErrConflict
	}
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, err := publisher.client.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		expected := current.DeepCopy()
		if err := setDeployment(expected, document, configMap.Name); err != nil {
			return err
		}
		if sameJSON(current.Spec, expected.Spec) {
			return nil
		}
		_, err = publisher.client.AppsV1().Deployments(namespace).Update(ctx, expected, metav1.UpdateOptions{})
		return err
	}); err != nil {
		return ErrConflict
	}
	return publisher.Check(ctx, document)
}

// Check сверяет применённый render, а не только подготовленный документ.
// Готовность слушателя проверяется отдельно в его Pod/readback.
func (publisher *Kubernetes) Check(ctx context.Context, document shared.Document) error {
	if document.Validate() != nil {
		return ErrInvalid
	}
	if err := publisher.CheckAdmission(ctx); err != nil {
		return err
	}
	configMap, network, err := projectionObjects(document)
	if err != nil {
		return err
	}
	if err := publisher.checkConfigMap(ctx, configMap); err != nil {
		return err
	}
	currentNetwork, err := publisher.client.NetworkingV1().NetworkPolicies(namespace).Get(ctx, shared.NetworkPolicyName, metav1.GetOptions{})
	if err != nil {
		return ErrUnavailable
	}
	if currentNetwork.UID == "" || currentNetwork.ResourceVersion == "" ||
		currentNetwork.Labels["app.kubernetes.io/name"] != deploymentName ||
		currentNetwork.Labels["app.kubernetes.io/component"] != "platform-egress" ||
		!sameJSON(currentNetwork.Spec, network.Spec) || currentNetwork.Annotations[generationAnnotation] != network.Annotations[generationAnnotation] ||
		currentNetwork.Annotations[sourceAnnotation] != network.Annotations[sourceAnnotation] {
		return ErrConflict
	}
	service, err := publisher.client.CoreV1().Services(namespace).Get(ctx, openAPIServiceName, metav1.GetOptions{})
	if err != nil {
		return ErrUnavailable
	}
	expectedService := service.DeepCopy()
	if setOpenAPIService(expectedService, document) != nil || !sameJSON(service.Spec, expectedService.Spec) ||
		!sameJSON(service.Annotations, expectedService.Annotations) {
		return ErrConflict
	}
	deployment, err := publisher.client.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		return ErrUnavailable
	}
	expected := deployment.DeepCopy()
	if setDeployment(expected, document, configMap.Name) != nil || !sameJSON(deployment.Spec, expected.Spec) || !deploymentReady(deployment) {
		return ErrConflict
	}
	return nil
}

func deploymentReady(deployment *appsv1.Deployment) bool {
	if deployment.UID == "" || deployment.ResourceVersion == "" || deployment.Generation < 1 ||
		deployment.Spec.Replicas == nil || *deployment.Spec.Replicas < 1 {
		return false
	}
	desired := *deployment.Spec.Replicas
	status := deployment.Status
	return status.ObservedGeneration == deployment.Generation && status.Replicas == desired &&
		status.UpdatedReplicas == desired && status.ReadyReplicas == desired && status.AvailableReplicas == desired && status.UnavailableReplicas == 0
}
