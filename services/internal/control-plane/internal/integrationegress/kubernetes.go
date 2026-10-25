// Package integrationegress публикует owner-проекцию точных OpenAPI origins.
package integrationegress

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	shared "github.com/codex-k8s/kodex/libs/go/integrationegresspolicy"
	"github.com/codex-k8s/kodex/libs/go/kubernetesadmission"
	"github.com/codex-k8s/kodex/libs/go/mailpolicy"
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
	// namespace pins every integration-egress Kubernetes operation to the system namespace rather than
	// accepting a caller-selected target namespace.
	// namespace закрепляет каждую integration-egress Kubernetes operation за system namespace вместо
	// выбранного caller target namespace.
	namespace = "kodex-system"
	// deploymentName pins the sole egress gateway Deployment/container that the projection may inspect
	// and update.
	// deploymentName закрепляет единственный egress gateway Deployment и container, которые проекция
	// может проверять и обновлять.
	deploymentName = "egress-gateway"
	// openAPIServiceName pins the separate ClusterIP Service that routes only the OpenAPI CONNECT
	// listener to the published generation.
	// openAPIServiceName закрепляет отдельный ClusterIP Service, направляющий только OpenAPI CONNECT
	// listener на опубликованное generation.
	openAPIServiceName = "egress-gateway-openapi"
	// generationLabel names the Pod/Service selector pin used to switch OpenAPI traffic to the exact
	// published generation.
	// generationLabel обозначает Pod и Service selector pin для переключения OpenAPI traffic на точное
	// опубликованное generation.
	generationLabel = "kodex.dev/integration-egress-generation"
	// generationAnnotation names the monotone generation fence stored on projection resources and
	// Deployment templates.
	// generationAnnotation обозначает монотонный generation fence в projection resources и Deployment
	// templates.
	generationAnnotation = "kodex.dev/integration-egress-generation"
	// sourceAnnotation names the source digest paired with a generation fence to reject
	// same-generation equivocation.
	// sourceAnnotation обозначает source digest, связанный с generation fence для отклонения
	// same-generation equivocation.
	sourceAnnotation = "kodex.dev/integration-egress-source-digest"
)

var (
	// ErrInvalid reports invalid document, client or rendered resource input through a fixed safe
	// error.
	// ErrInvalid обозначает неверные document, client либо rendered resource input фиксированной
	// безопасной ошибкой.
	ErrInvalid = errors.New("integration egress projection is invalid")
	// ErrConflict reports mismatched source/fence/readback or rejected reconciliation without
	// reflecting Kubernetes response bodies.
	// ErrConflict обозначает несовпадение source, fence, readback либо отклонённое reconciliation без
	// отражения Kubernetes response bodies.
	ErrConflict = errors.New("integration egress projection conflicts with current generation")
	// ErrUnavailable reports an unavailable Kubernetes dependency or resource through a fixed error
	// without forwarding provider details.
	// ErrUnavailable обозначает недоступную Kubernetes dependency либо resource фиксированной ошибкой
	// без передачи provider details.
	ErrUnavailable = errors.New("integration egress projection is unavailable")
)

// Kubernetes holds the injected Kubernetes client for fixed-target projection operations without
// owning a background reconciler.
// Kubernetes хранит внедрённый Kubernetes client для fixed-target projection operations без
// владения фоновым reconciler.
type Kubernetes struct{ client kubernetes.Interface }

// InCluster builds an in-cluster client with the supplied request timeout and returns a fixed
// unavailable error on setup failure.
// InCluster создаёт in-cluster client с переданным request timeout и возвращает фиксированный
// unavailable при setup failure.
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

// New requires a nonnil injected client without contacting the cluster or applying a projection.
// New требует ненулевой внедрённый client без обращения к кластеру либо применения проекции.
func New(client kubernetes.Interface) (*Kubernetes, error) {
	if client == nil {
		return nil, ErrInvalid
	}
	return &Kubernetes{client: client}, nil
}

// sameJSON compares marshaled JSON bytes with failure mapped to false, preserving existing
// nil/empty distinctions rather than normalizing resource specs.
// sameJSON сравнивает marshaled JSON bytes с false при ошибке, сохраняя прежние различия nil и
// empty вместо нормализации resource specs.
func sameJSON(left, right any) bool { return kubernetesadmission.SameJSON(left, right) }

// CheckAdmission reads all three registered admission policies/bindings and requires exact specs,
// observed generation, identity/version and warning-free type checking before publication.
// CheckAdmission читает все три зарегистрированные admission policies и bindings и требует точные
// specs, observed generation, identity и version и type checking без warnings до публикации.
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
		if err := kubernetesadmission.Check(ctx, publisher.client.AdmissionregistrationV1(), item.name, item.source, kubernetesadmission.Failures{Invalid: ErrInvalid, Unavailable: ErrUnavailable, Conflict: ErrConflict}); err != nil {
			return err
		}
	}
	return nil
}

// projectionObjects renders the document into typed immutable ConfigMap and supplemental
// NetworkPolicy objects with exact generation/source annotations.
// projectionObjects преобразует document в типизированные immutable ConfigMap и supplemental
// NetworkPolicy объекты с точными generation и source annotations.
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

// checkFence allows an absent fence only for initial generation one and rejects malformed, newer
// or same-generation/different-source annotations.
// checkFence допускает отсутствующий fence только для initial generation one и отклоняет
// повреждённые, более новые либо same-generation с другим source annotations.
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

// checkNetworkFence accepts a present generation fence or the exact empty repo-owned supplemental
// egress bootstrap shape; an arbitrary unfenced/open policy cannot recover.
// checkNetworkFence принимает существующий generation fence либо точную пустую repo-owned
// supplemental egress bootstrap форму; произвольная unfenced либо открытая policy не допускает
// recovery.
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

// checkGatewaySource requires the fixed gateway identity/labels, one exact container, base digest,
// policy file, CONNECT listener/port and readonly nonsubpath mount before updating projection
// pins.
// checkGatewaySource требует фиксированные gateway identity и labels, один точный container, base
// digest, policy file, CONNECT listener и port и readonly nonsubpath mount до обновления
// projection pins.
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

// checkOpenAPIServiceSource requires the fixed private single-port Service and exact
// selector/fence, allowing only the canonical generation-one selector when bootstrap annotations
// are absent.
// checkOpenAPIServiceSource требует фиксированный private single-port Service и точные selector и
// fence, допуская только canonical generation-one selector при отсутствии bootstrap annotations.
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

// setOpenAPIService validates the owner document and source Service before updating only the exact
// generation selector and generation/source annotations.
// setOpenAPIService проверяет owner document и source Service до обновления только точного
// generation selector и generation и source annotations.
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

// setDeployment validates document/source/fence before setting exact generation labels, digest
// environment and immutable policy volume, rejecting missing or duplicate owned targets.
// setDeployment проверяет document, source и fence до установки точных generation labels, digest
// environment и immutable policy volume, отклоняя missing либо duplicate owned targets.
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

// checkConfigMap requires observed UID/version, immutable text data without binary content/owner
// refs and the exact allowed projection label set.
// checkConfigMap требует наблюдаемые UID и version, immutable text data без binary content и owner
// refs и точный допустимый набор projection labels.
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

// validProjectionLabels requires expected identity labels and permits only exact repo-owned local
// provenance additions, rejecting unknown keys or profile values.
// validProjectionLabels требует ожидаемые identity labels и допускает только точные repo-owned
// local provenance дополнения, отклоняя неизвестные keys либо profile values.
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

// Publish delegates ordered publication and complete Kubernetes readback to PublishWithStage while
// discarding only its diagnostic stage.
// Publish передаёт последовательную публикацию и полный Kubernetes readback в PublishWithStage,
// отбрасывая только diagnostic stage.
func (publisher *Kubernetes) Publish(ctx context.Context, document shared.Document) error {
	_, err := publisher.PublishWithStage(ctx, document)
	return err
}

// PublishWithStage validates admission/source before ordered immutable ConfigMap, network, Service
// and Deployment reconciliation, then exact readback; closed stages identify failure and do not
// make multi-object writes atomic.
// PublishWithStage проверяет admission и source до последовательного immutable ConfigMap, network,
// Service и Deployment reconciliation, затем точного readback; закрытые stages обозначают отказ и
// не делают multi-object writes атомарными.
func (publisher *Kubernetes) PublishWithStage(ctx context.Context, document shared.Document) (string, error) {
	if document.Validate() != nil {
		return "document", ErrInvalid
	}
	if err := publisher.CheckAdmission(ctx); err != nil {
		return "admission", err
	}
	configMap, network, err := projectionObjects(document)
	if err != nil {
		return "render", err
	}
	current, err := publisher.client.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		return "deployment_source", ErrUnavailable
	}
	if checkGatewaySource(current, document) != nil || checkFence(current.Spec.Template.Annotations, document) != nil {
		return "deployment_source", ErrConflict
	}
	if _, err := publisher.client.CoreV1().ConfigMaps(namespace).Create(ctx, &configMap, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return "configmap_create", ErrUnavailable
	}
	if err := publisher.checkConfigMap(ctx, configMap); err != nil {
		return "configmap_readback", err
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
		return "network_policy", ErrConflict
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
		return "service", ErrConflict
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
		return "deployment", ErrConflict
	}
	stage, err := publisher.CheckWithStage(ctx, document)
	return "readback_" + stage, err
}

// Check delegates applied-object and replica readback to CheckWithStage; listener reachability
// remains a separate Pod/runtime check.
// Check передаёт проверку применённых объектов и replica readback в CheckWithStage; достижимость
// listener остаётся отдельной Pod и runtime проверкой.
func (publisher *Kubernetes) Check(ctx context.Context, document shared.Document) error {
	_, err := publisher.CheckWithStage(ctx, document)
	return err
}

// CheckWithStage rechecks document/admission, exact immutable policy/network/Service/Deployment
// state and all replica counters, returning only a closed stage with the safe error.
// CheckWithStage повторно проверяет document, admission, точные immutable policy, network, Service
// и Deployment state и все replica counters, возвращая только закрытый stage с безопасной ошибкой.
func (publisher *Kubernetes) CheckWithStage(ctx context.Context, document shared.Document) (string, error) {
	if document.Validate() != nil {
		return "document", ErrInvalid
	}
	if err := publisher.CheckAdmission(ctx); err != nil {
		return "admission", err
	}
	configMap, network, err := projectionObjects(document)
	if err != nil {
		return "render", err
	}
	if err := publisher.checkConfigMap(ctx, configMap); err != nil {
		return "configmap", err
	}
	currentNetwork, err := publisher.client.NetworkingV1().NetworkPolicies(namespace).Get(ctx, shared.NetworkPolicyName, metav1.GetOptions{})
	if err != nil {
		return "network_policy", ErrUnavailable
	}
	if currentNetwork.UID == "" || currentNetwork.ResourceVersion == "" ||
		currentNetwork.Labels["app.kubernetes.io/name"] != deploymentName ||
		currentNetwork.Labels["app.kubernetes.io/component"] != "platform-egress" ||
		!sameJSON(currentNetwork.Spec, network.Spec) || currentNetwork.Annotations[generationAnnotation] != network.Annotations[generationAnnotation] ||
		currentNetwork.Annotations[sourceAnnotation] != network.Annotations[sourceAnnotation] {
		return "network_policy", ErrConflict
	}
	service, err := publisher.client.CoreV1().Services(namespace).Get(ctx, openAPIServiceName, metav1.GetOptions{})
	if err != nil {
		return "service", ErrUnavailable
	}
	expectedService := service.DeepCopy()
	if setOpenAPIService(expectedService, document) != nil || !sameJSON(service.Spec, expectedService.Spec) ||
		!sameJSON(service.Annotations, expectedService.Annotations) {
		return "service", ErrConflict
	}
	deployment, err := publisher.client.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		return "deployment", ErrUnavailable
	}
	expected := deployment.DeepCopy()
	if setDeployment(expected, document, configMap.Name) != nil {
		return "deployment_source", ErrConflict
	}
	if !sameJSON(deployment.Spec, expected.Spec) {
		return "deployment_spec", ErrConflict
	}
	if !deploymentReady(deployment) {
		return "deployment_ready", ErrConflict
	}
	return "complete", nil
}

// deploymentReady requires observed Deployment identity/version, positive desired replicas and
// exact generation/replica agreement with no unavailable replicas.
// deploymentReady требует наблюдаемые Deployment identity и version, положительные desired
// replicas и точное generation и replica соответствие без unavailable replicas.
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
