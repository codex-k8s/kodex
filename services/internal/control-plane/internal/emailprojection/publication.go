package emailprojection

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/libs/go/mailpolicy"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

const (
	// publicationRevisionAnnotation names the mailbox revision fence stored on published network
	// policy and Deployment templates.
	// publicationRevisionAnnotation обозначает mailbox revision fence, сохраняемый в публикуемых
	// network policy и Deployment templates.
	publicationRevisionAnnotation = "kodex.dev/mail-configuration-revision"
	// publicationDigestAnnotation names the exact configuration digest paired with each mailbox
	// revision fence.
	// publicationDigestAnnotation обозначает точный configuration digest, связанный с каждым mailbox
	// revision fence.
	publicationDigestAnnotation = "kodex.dev/mail-configuration-digest"
	// mailPolicyNetworkName pins the existing supplemental egress policy updated for mailbox
	// publication.
	// mailPolicyNetworkName закрепляет существующую дополнительную egress policy, обновляемую при
	// mailbox publication.
	mailPolicyNetworkName = "egress-gateway-mail-destinations"
)

// samePublicationJSON compares serialized JSON bytes and returns false on either marshal failure,
// preserving the existing nil/empty encoding distinction.
// samePublicationJSON сравнивает сериализованные JSON bytes и возвращает false при ошибке любого
// marshal, сохраняя прежнее различие кодирования nil и empty.
func samePublicationJSON(left, right any) bool {
	a, err := json.Marshal(left)
	if err != nil {
		return false
	}
	b, err := json.Marshal(right)
	return err == nil && bytes.Equal(a, b)
}

// CheckPublicationAdmission requires the fixed namespace and observed warning-free admission
// policy plus exact expected binding/spec before any mailbox publication effect.
// CheckPublicationAdmission требует фиксированный namespace и наблюдаемую admission policy без
// warnings с точными ожидаемыми binding и spec до любого mailbox publication effect.
func (publisher *Kubernetes) CheckPublicationAdmission(ctx context.Context) error {
	if publisher.namespace != "kodex-system" {
		return ErrInvalid
	}
	policy, err := publisher.client.AdmissionregistrationV1().ValidatingAdmissionPolicies().Get(ctx, mailpolicy.PublicationAdmissionName, metav1.GetOptions{})
	if err != nil {
		return ErrUnavailable
	}
	binding, err := publisher.client.AdmissionregistrationV1().ValidatingAdmissionPolicyBindings().Get(ctx, mailpolicy.PublicationAdmissionName, metav1.GetOptions{})
	if err != nil {
		return ErrUnavailable
	}
	policySource, bindingSource := mailpolicy.PublicationAdmissionResources()
	policyRaw, _ := json.Marshal(policySource)
	bindingRaw, _ := json.Marshal(bindingSource)
	var expectedPolicy admissionv1.ValidatingAdmissionPolicy
	var expectedBinding admissionv1.ValidatingAdmissionPolicyBinding
	if json.Unmarshal(policyRaw, &expectedPolicy) != nil || json.Unmarshal(bindingRaw, &expectedBinding) != nil {
		return ErrInvalid
	}
	if policy.UID == "" || policy.ResourceVersion == "" || policy.Generation < 1 || policy.Status.ObservedGeneration != policy.Generation || policy.Status.TypeChecking == nil || len(policy.Status.TypeChecking.ExpressionWarnings) != 0 ||
		binding.UID == "" || binding.ResourceVersion == "" || !samePublicationJSON(policy.Spec, expectedPolicy.Spec) || !samePublicationJSON(binding.Spec, expectedBinding.Spec) {
		return ErrConflict
	}
	return nil
}

// publicationObjects renders the validated mail document into typed ConfigMap/network resources
// and attaches exact configuration revision/digest annotations.
// publicationObjects преобразует проверенный mail document в типизированные ConfigMap и network
// resources и добавляет точные configuration revision и digest annotations.
func publicationObjects(document mailpolicy.MailDocument) (corev1.ConfigMap, networkingv1.NetworkPolicy, error) {
	var configMap corev1.ConfigMap
	var policy networkingv1.NetworkPolicy
	files, err := mailpolicy.RenderFiles(document)
	if err != nil || json.Unmarshal(files["mail-configmap.json"], &configMap) != nil || json.Unmarshal(files["mail-networkpolicy.json"], &policy) != nil {
		return configMap, policy, ErrInvalid
	}
	policy.Annotations = map[string]string{publicationRevisionAnnotation: strconv.FormatInt(document.ConfigurationRevision, 10), publicationDigestAnnotation: document.ConfigurationDigest}
	return configMap, policy, nil
}

// checkPublicationFence rejects unparsable, newer or equivocal existing revision fences while
// retaining the original acceptance of a missing fence.
// checkPublicationFence отклоняет неразбираемый, более новый либо неоднозначный existing revision
// fence, сохраняя исходный допуск отсутствующего fence.
func checkPublicationFence(annotations map[string]string, document mailpolicy.MailDocument) error {
	if raw := annotations[publicationRevisionAnnotation]; raw != "" {
		revision, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || revision > document.ConfigurationRevision || revision == document.ConfigurationRevision && annotations[publicationDigestAnnotation] != document.ConfigurationDigest {
			return ErrConflict
		}
	}
	return nil
}

// PublishMailbox checks document/configuration pins and admission before ordered ConfigMap,
// network, gateway, Secret and email Deployment writes; the Secret receipt does not replace later
// CheckMailbox replica readback or make the writes atomic.
// PublishMailbox проверяет document и configuration pins и admission до последовательных
// ConfigMap, network, gateway, Secret и email Deployment writes; Secret receipt не заменяет
// последующий CheckMailbox replica readback и не делает writes атомарными.
func (publisher *Kubernetes) PublishMailbox(ctx context.Context, configuration api.Configuration, credentials map[string]string, document mailpolicy.MailDocument) (Receipt, error) {
	if document.Validate() != nil || document.ConfigurationRevision != configuration.Revision || document.ConfigurationDigest != api.Digest(configuration) {
		return Receipt{}, ErrInvalid
	}
	if err := publisher.CheckPublicationAdmission(ctx); err != nil {
		return Receipt{}, err
	}
	gateway, err := publisher.client.AppsV1().Deployments(publisher.namespace).Get(ctx, "egress-gateway", metav1.GetOptions{})
	if err != nil {
		return Receipt{}, ErrUnavailable
	}
	if err := checkMailGatewaySource(gateway, document); err != nil {
		return Receipt{}, err
	}
	configMap, network, err := publicationObjects(document)
	if err != nil {
		return Receipt{}, err
	}
	if _, err := publisher.client.CoreV1().ConfigMaps(publisher.namespace).Create(ctx, &configMap, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return Receipt{}, ErrUnavailable
	}
	if err := publisher.checkMailConfigMap(ctx, configMap); err != nil {
		return Receipt{}, err
	}
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, err := publisher.client.NetworkingV1().NetworkPolicies(publisher.namespace).Get(ctx, mailPolicyNetworkName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if err := checkPublicationFence(current.Annotations, document); err != nil {
			return err
		}
		if samePublicationJSON(current.Spec, network.Spec) && current.Annotations[publicationRevisionAnnotation] == network.Annotations[publicationRevisionAnnotation] && current.Annotations[publicationDigestAnnotation] == document.ConfigurationDigest {
			return nil
		}
		current.Spec = network.Spec
		if current.Annotations == nil {
			current.Annotations = map[string]string{}
		}
		for key, value := range network.Annotations {
			current.Annotations[key] = value
		}
		_, err = publisher.client.NetworkingV1().NetworkPolicies(publisher.namespace).Update(ctx, current, metav1.UpdateOptions{})
		return err
	}); err != nil {
		return Receipt{}, ErrConflict
	}
	if err := publisher.applyMailDeployment(ctx, "egress-gateway", document, configMap.Name); err != nil {
		return Receipt{}, err
	}
	receipt, err := publisher.Publish(ctx, configuration, credentials)
	if err != nil {
		return Receipt{}, err
	}
	if err := publisher.applyMailDeployment(ctx, "email-bridge", document, configMap.Name); err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

// publicationEnvironment returns the gateway mail digest pin or the bridge
// managed-mode/revision/digest pins used by the selected Deployment update.
// publicationEnvironment возвращает gateway mail digest pin либо bridge managed-mode, revision и
// digest pins для выбранного Deployment update.
func publicationEnvironment(name string, document mailpolicy.MailDocument) map[string]string {
	if name == "egress-gateway" {
		return map[string]string{"EGRESS_GATEWAY_MAIL_POLICY_DIGEST": document.Digest()}
	}
	return map[string]string{"EMAIL_BRIDGE_CONFIGURATION_MODE": "managed", "EMAIL_BRIDGE_EXPECTED_CONFIGURATION_REVISION": strconv.FormatInt(document.ConfigurationRevision, 10),
		"EMAIL_BRIDGE_EXPECTED_CONFIGURATION_DIGEST": document.ConfigurationDigest, "EMAIL_BRIDGE_EGRESS_POLICY_DIGEST": document.Digest()}
}

// setMailDeployment guards the existing revision/source then updates the single named container
// pins in stable order and the gateway mail ConfigMap volume, rejecting duplicate or missing
// targets.
// setMailDeployment проверяет existing revision и source, затем обновляет pins единственного
// именованного container в стабильном порядке и gateway mail ConfigMap volume, отклоняя duplicate
// либо missing targets.
func setMailDeployment(deployment *appsv1.Deployment, name string, document mailpolicy.MailDocument, configMapName string) error {
	if name == "egress-gateway" {
		if err := checkMailGatewaySource(deployment, document); err != nil {
			return err
		}
	}
	if err := checkPublicationFence(deployment.Spec.Template.Annotations, document); err != nil {
		return err
	}
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = map[string]string{}
	}
	deployment.Spec.Template.Annotations[publicationRevisionAnnotation] = strconv.FormatInt(document.ConfigurationRevision, 10)
	deployment.Spec.Template.Annotations[publicationDigestAnnotation] = document.ConfigurationDigest
	wanted := publicationEnvironment(name, document)
	found := 0
	for index := range deployment.Spec.Template.Spec.Containers {
		container := &deployment.Spec.Template.Spec.Containers[index]
		if container.Name != name {
			continue
		}
		found++
		seen := map[string]bool{}
		for i := range container.Env {
			item := &container.Env[i]
			if value, ok := wanted[item.Name]; ok {
				if seen[item.Name] {
					return ErrConflict
				}
				seen[item.Name] = true
				item.Value, item.ValueFrom = value, nil
			}
		}
		// Стабильный порядок не создаёт новый rollout при повторе публикации.
		for _, key := range []string{"EGRESS_GATEWAY_MAIL_POLICY_DIGEST", "EMAIL_BRIDGE_CONFIGURATION_MODE", "EMAIL_BRIDGE_EXPECTED_CONFIGURATION_REVISION", "EMAIL_BRIDGE_EXPECTED_CONFIGURATION_DIGEST", "EMAIL_BRIDGE_EGRESS_POLICY_DIGEST"} {
			if value, ok := wanted[key]; ok && !seen[key] {
				container.Env = append(container.Env, corev1.EnvVar{Name: key, Value: value})
			}
		}
	}
	if found != 1 {
		return ErrConflict
	}
	if name == "egress-gateway" {
		found = 0
		for index := range deployment.Spec.Template.Spec.Volumes {
			volume := &deployment.Spec.Template.Spec.Volumes[index]
			if volume.Name != "mail-policy" {
				continue
			}
			found++
			mode := int32(0444)
			volume.VolumeSource = corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: configMapName}, DefaultMode: &mode, Items: []corev1.KeyToPath{{Key: "mail-policy.json", Path: "mail-policy.json"}}}}
		}
		if found != 1 {
			return ErrConflict
		}
	}
	return nil
}

// checkMailGatewaySource requires one gateway container with exact base digest, policy file and
// readonly mount without subpaths before changing its mail-policy source.
// checkMailGatewaySource требует один gateway container с точными base digest, policy file и
// readonly mount без subpaths до изменения mail-policy source.
func checkMailGatewaySource(deployment *appsv1.Deployment, document mailpolicy.MailDocument) error {
	containers, pins, files, mounts := 0, 0, 0, 0
	for _, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name != "egress-gateway" {
			continue
		}
		containers++
		for _, item := range container.Env {
			if item.Name == "EGRESS_GATEWAY_EXPECTED_POLICY_DIGEST" {
				pins++
				if item.ValueFrom != nil || item.Value != document.GatewayPolicyDigest {
					return ErrConflict
				}
			}
			if item.Name == "EGRESS_GATEWAY_MAIL_POLICY_FILE" {
				files++
				if item.ValueFrom != nil || item.Value != "/var/run/config/kodex/egress-gateway-mail/mail-policy.json" {
					return ErrConflict
				}
			}
		}
		for _, mount := range container.VolumeMounts {
			if mount.Name == "mail-policy" {
				mounts++
				if mount.MountPath != "/var/run/config/kodex/egress-gateway-mail" || !mount.ReadOnly || mount.SubPath != "" || mount.SubPathExpr != "" {
					return ErrConflict
				}
			}
		}
	}
	if containers != 1 || pins != 1 || files != 1 || mounts != 1 {
		return ErrConflict
	}
	return nil
}

// applyMailDeployment re-reads and transforms the selected Deployment on conflict retries,
// skipping an identical spec and mapping failure to a fixed conflict without asserting rollout
// completion.
// applyMailDeployment повторно читает и преобразует выбранный Deployment при conflict retries,
// пропускает идентичный spec и отображает отказ в фиксированный conflict без утверждения
// завершения rollout.
func (publisher *Kubernetes) applyMailDeployment(ctx context.Context, name string, document mailpolicy.MailDocument, configMapName string) error {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, err := publisher.client.AppsV1().Deployments(publisher.namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		expected := current.DeepCopy()
		if err := setMailDeployment(expected, name, document, configMapName); err != nil {
			return err
		}
		if samePublicationJSON(current.Spec, expected.Spec) {
			return nil
		}
		_, err = publisher.client.AppsV1().Deployments(publisher.namespace).Update(ctx, expected, metav1.UpdateOptions{})
		return err
	})
	if err != nil {
		return ErrConflict
	}
	return nil
}

// checkMailConfigMap requires observed UID/version, immutable text data without binary entries and
// every expected label while retaining the original allowance for additional labels.
// checkMailConfigMap требует наблюдаемые UID и version, immutable text data без binary entries и
// каждый expected label, сохраняя исходный допуск дополнительных labels.
func (publisher *Kubernetes) checkMailConfigMap(ctx context.Context, expected corev1.ConfigMap) error {
	current, err := publisher.client.CoreV1().ConfigMaps(publisher.namespace).Get(ctx, expected.Name, metav1.GetOptions{})
	if err != nil {
		return ErrUnavailable
	}
	if current.UID == "" || current.ResourceVersion == "" || current.Immutable == nil || !*current.Immutable || len(current.BinaryData) != 0 || !samePublicationJSON(current.Data, expected.Data) {
		return ErrConflict
	}
	for key, value := range expected.Labels {
		if current.Labels[key] != value {
			return ErrConflict
		}
	}
	return nil
}

// CheckMailbox reads admission, immutable policy data, exact network fences, both desired/observed
// Deployment states and the Secret receipt; it does not test a live SMTP/IMAP connection.
// CheckMailbox читает admission, immutable policy data, точные network fences, desired и observed
// состояния обоих Deployment и Secret receipt; живое SMTP либо IMAP соединение не проверяет.
func (publisher *Kubernetes) CheckMailbox(ctx context.Context, configuration api.Configuration, credentials map[string]string, document mailpolicy.MailDocument) (Receipt, error) {
	if document.Validate() != nil || document.ConfigurationRevision != configuration.Revision || document.ConfigurationDigest != api.Digest(configuration) {
		return Receipt{}, ErrInvalid
	}
	if err := publisher.CheckPublicationAdmission(ctx); err != nil {
		return Receipt{}, err
	}
	configMap, network, err := publicationObjects(document)
	if err != nil {
		return Receipt{}, err
	}
	if err := publisher.checkMailConfigMap(ctx, configMap); err != nil {
		return Receipt{}, err
	}
	current, err := publisher.client.NetworkingV1().NetworkPolicies(publisher.namespace).Get(ctx, network.Name, metav1.GetOptions{})
	if err != nil {
		return Receipt{}, ErrUnavailable
	}
	if current.UID == "" || current.ResourceVersion == "" || !samePublicationJSON(current.Spec, network.Spec) || current.Annotations[publicationRevisionAnnotation] != network.Annotations[publicationRevisionAnnotation] || current.Annotations[publicationDigestAnnotation] != document.ConfigurationDigest {
		return Receipt{}, ErrConflict
	}
	for _, name := range []string{"egress-gateway", "email-bridge"} {
		deployment, err := publisher.client.AppsV1().Deployments(publisher.namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return Receipt{}, ErrUnavailable
		}
		expected := deployment.DeepCopy()
		if setMailDeployment(expected, name, document, configMap.Name) != nil || !samePublicationJSON(deployment.Spec, expected.Spec) || !mailDeploymentReady(deployment) {
			return Receipt{}, ErrConflict
		}
	}
	return publisher.Check(ctx, configuration, credentials)
}

// mailDeploymentReady requires nonempty Deployment identity/version, positive desired replicas and
// exact observed-generation/replica counters with zero unavailable replicas.
// mailDeploymentReady требует непустые Deployment identity и version, положительные desired
// replicas и точные observed-generation и replica counters с нулевыми unavailable replicas.
func mailDeploymentReady(deployment *appsv1.Deployment) bool {
	if deployment.UID == "" || deployment.ResourceVersion == "" || deployment.Generation < 1 || deployment.Spec.Replicas == nil || *deployment.Spec.Replicas < 1 {
		return false
	}
	desired := *deployment.Spec.Replicas
	status := deployment.Status
	return status.ObservedGeneration == deployment.Generation && status.Replicas == desired && status.UpdatedReplicas == desired && status.ReadyReplicas == desired && status.AvailableReplicas == desired && status.UnavailableReplicas == 0
}
